package vision

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	SocketPath      = "/run/synora/vision-worker.sock"
	connectAttempts = 5
	connectDelay    = 100 * time.Millisecond
)

type Runtime struct {
	manager *WorkerManager

	conn net.Conn

	reader *bufio.Reader

	mu sync.Mutex
}

type Request struct {
	ID string `json:"id"`

	ClipPath string `json:"clip_path"`

	CameraID string `json:"camera_id"`
}

type Event struct {
	Type string `json:"type"`

	Source string `json:"source"`

	SceneID string `json:"scene_id,omitempty"`

	TrackID any `json:"track_id,omitempty"`

	Timestamp float64 `json:"timestamp"`

	Version int `json:"version,omitempty"`

	Payload map[string]any `json:"payload"`
}

type WorkerResponse struct {
	Events []Event `json:"events"`

	Error string `json:"error,omitempty"`
}

func NewRuntime() *Runtime {
	return NewRuntimeWithManager(
		NewWorkerManager(
			nil,
			WorkerManagerConfig{},
		),
	)
}

func NewRuntimeWithManager(
	manager *WorkerManager,
) *Runtime {
	return &Runtime{
		manager: manager,
	}
}

func (v *Runtime) Start() error {
	if err := v.manager.Start(
		"discovery",
	); err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.connect(); err != nil {
		v.manager.PublishUnavailable(err.Error())
		return err
	}
	return nil
}

func (v *Runtime) connect() error {
	if v.conn != nil {
		return nil
	}

	var conn net.Conn
	var err error

	for i := 0; i < connectAttempts; i++ {

		conn, err = net.Dial(
			"unix",
			SocketPath,
		)

		if err == nil {
			break
		}

		time.Sleep(connectDelay)
	}

	if err != nil {

		return fmt.Errorf(
			"failed to connect vision worker socket: %w",
			err,
		)
	}

	v.conn = conn

	v.reader = bufio.NewReader(
		conn,
	)

	return nil
}

func (v *Runtime) Snapshot() WorkerSnapshot {
	if v == nil || v.manager == nil {
		return WorkerSnapshot{Status: WorkerStatusStopped}
	}
	return v.manager.Snapshot()
}

func (v *Runtime) PublishUnavailable(reason string) {
	if v == nil || v.manager == nil {
		return
	}
	v.manager.PublishUnavailable(reason)
}

func (v *Runtime) Process(
	job *ClipJob,
) (*WorkerResponse, error) {
	returnValue := &WorkerResponse{}

	err := v.manager.WithCamera(
		job.CameraID,
		func() error {
			resp, err := v.processLocked(
				job,
			)

			if err != nil {
				return err
			}

			*returnValue = *resp

			return nil
		},
	)

	if err != nil {
		return nil, err
	}

	return returnValue, nil
}

func (v *Runtime) processLocked(
	job *ClipJob,
) (*WorkerResponse, error) {

	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.connect(); err != nil {
		return nil, err
	}

	req := Request{
		ID: job.ID,

		ClipPath: job.Path,

		CameraID: job.CameraID,
	}

	err := json.NewEncoder(
		v.conn,
	).Encode(req)

	if err != nil {
		v.closeConn()

		return nil, err
	}

	var resp WorkerResponse

	err = json.NewDecoder(
		v.reader,
	).Decode(&resp)

	if err != nil {
		v.closeConn()

		return nil, err
	}

	if resp.Error != "" {

		return nil, fmt.Errorf(
			"vision worker error: %s",
			resp.Error,
		)
	}

	return &resp, nil
}

func (v *Runtime) closeConn() {
	if v.conn != nil {
		_ = v.conn.Close()
	}

	v.conn = nil
	v.reader = nil
}
