package vision

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	SocketPath = "/run/synora/vision-worker.sock"
)

type Runtime struct {
	cmd *exec.Cmd

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

	return &Runtime{}
}

func (v *Runtime) Start() error {

	_ = os.Remove(
		SocketPath,
	)

	v.cmd = exec.Command(
		"/opt/synora/venv/bin/python",
		"/opt/synora/services/vision-worker/worker.py",
	)

	v.cmd.Stdout = os.Stdout
	v.cmd.Stderr = os.Stderr

	err := v.cmd.Start()

	if err != nil {

		return err
	}

	var conn net.Conn

	for i := 0; i < 50; i++ {

		conn, err = net.Dial(
			"unix",
			SocketPath,
		)

		if err == nil {
			break
		}

		time.Sleep(
			100 * time.Millisecond,
		)
	}

	if err != nil {

		return fmt.Errorf(
			"failed to connect vision worker: %w",
			err,
		)
	}

	v.conn = conn

	v.reader = bufio.NewReader(
		conn,
	)

	return nil
}

func (v *Runtime) Process(
	job *ClipJob,
) (*WorkerResponse, error) {

	v.mu.Lock()
	defer v.mu.Unlock()

	req := Request{
		ID: job.ID,

		ClipPath: job.Path,

		CameraID: job.CameraID,
	}

	err := json.NewEncoder(
		v.conn,
	).Encode(req)

	if err != nil {

		return nil, err
	}

	var resp WorkerResponse

	err = json.NewDecoder(
		v.reader,
	).Decode(&resp)

	if err != nil {

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
