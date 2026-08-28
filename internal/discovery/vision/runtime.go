package vision

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"synora/internal/facedataset"
	"synora/internal/idgen"
	"synora/internal/runtimeconfig"
	"synora/pkg/contract"
)

const (
	SocketPath      = runtimeconfig.DefaultVisionWorkerSocket
	connectAttempts = 5
	connectDelay    = 100 * time.Millisecond
)

type Runtime struct {
	manager       *WorkerManager
	socketPath    string
	workerTimeout time.Duration

	conn net.Conn

	reader *bufio.Reader

	mu sync.Mutex
}

type Request struct {
	ID           string `json:"id"`
	ActivationID string `json:"activation_id,omitempty"`
	ClipIndex    int    `json:"clip_index,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	SequenceKey  string `json:"sequence_key,omitempty"`
	TrackID      string `json:"track_id,omitempty"`

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

// FaceDatasetRequest is the internal, typed reload boundary. The worker must
// load and validate the complete immutable version before swapping its FaceDB.
// Version is the only externally meaningful identifier; Root is supplied by
// Discovery and is never accepted from an HTTP client.
type FaceDatasetRequest struct {
	RequestID string `json:"request_id"`
	Operation string `json:"operation"`
	Version   string `json:"version"`
	Root      string `json:"root"`
}

type FaceDatasetResponse struct {
	RequestID        string `json:"request_id"`
	Version          string `json:"version"`
	LoadedVersion    string `json:"loaded_version,omitempty"`
	ActiveRevision   uint64 `json:"active_revision,omitempty"`
	Dimension        int    `json:"dimension,omitempty"`
	ModelFingerprint string `json:"model_fingerprint,omitempty"`
	Fingerprint      string `json:"fingerprint,omitempty"`
	Error            string `json:"error,omitempty"`
	FailureCode      string `json:"failure_code,omitempty"`
}

type FaceEmbeddingRequest struct {
	RequestID  string `json:"request_id"`
	Operation  string `json:"operation"`
	ResidentID string `json:"resident_id"`
	PhotoID    string `json:"photo_id"`
	StorageKey string `json:"storage_key"`
}

type FaceEmbeddingResponse struct {
	RequestID        string    `json:"request_id"`
	Embedding        []float32 `json:"embedding"`
	ModelFingerprint string    `json:"model_fingerprint,omitempty"`
	Error            string    `json:"error,omitempty"`
	FailureCode      string    `json:"failure_code,omitempty"`
}

func (v *Runtime) ReloadFaceDataset(ctx context.Context, version, root string) (facedataset.ReloadResult, error) {
	if v == nil {
		return facedataset.ReloadResult{}, fmt.Errorf("vision runtime unavailable")
	}
	if strings.TrimSpace(version) == "" || strings.TrimSpace(root) == "" {
		return facedataset.ReloadResult{}, fmt.Errorf("dataset version and root are required")
	}
	select {
	case <-ctx.Done():
		return facedataset.ReloadResult{}, ctx.Err()
	default:
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.connect(); err != nil {
		return facedataset.ReloadResult{}, err
	}
	conn := v.conn
	if err := conn.SetDeadline(time.Now().UTC().Add(v.workerTimeout)); err != nil {
		v.closeConn()
		return facedataset.ReloadResult{}, err
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	requestID := idgen.New("face-reload")
	request := FaceDatasetRequest{RequestID: requestID, Operation: "face_dataset.reload", Version: version, Root: root}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		v.closeConn()
		return facedataset.ReloadResult{}, err
	}
	var response FaceDatasetResponse
	if err := json.NewDecoder(v.reader).Decode(&response); err != nil {
		v.closeConn()
		return facedataset.ReloadResult{}, err
	}
	if response.Error != "" {
		if response.FailureCode != "" {
			return facedataset.ReloadResult{}, fmt.Errorf("vision face dataset reload failed code=%s: %s", response.FailureCode, response.Error)
		}
		return facedataset.ReloadResult{}, fmt.Errorf("vision face dataset reload failed: %s", response.Error)
	}
	if response.RequestID != requestID || response.Version == "" || (response.LoadedVersion != "" && response.LoadedVersion != response.Version) {
		return facedataset.ReloadResult{}, fmt.Errorf("vision face dataset reload returned no version")
	}
	fingerprint := response.ModelFingerprint
	if fingerprint == "" {
		fingerprint = response.Fingerprint
	}
	return facedataset.ReloadResult{Version: response.Version, ActiveRevision: response.ActiveRevision, EmbeddingDimension: response.Dimension, ModelFingerprint: fingerprint}, nil
}

func (v *Runtime) Embed(ctx context.Context, path string, photo contract.FacePhoto) ([]float32, string, error) {
	if v == nil {
		return nil, "", fmt.Errorf("vision runtime unavailable")
	}
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	default:
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.connect(); err != nil {
		return nil, "", err
	}
	conn := v.conn
	if err := conn.SetDeadline(time.Now().UTC().Add(v.workerTimeout)); err != nil {
		v.closeConn()
		return nil, "", err
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	_ = path // The worker resolves storage_key under its canonical root.
	requestID := idgen.New("face-embed")
	request := FaceEmbeddingRequest{RequestID: requestID, Operation: "face_dataset.embed", ResidentID: photo.ResidentID, PhotoID: photo.ID, StorageKey: photo.StorageKey}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		v.closeConn()
		return nil, "", err
	}
	var response FaceEmbeddingResponse
	if err := json.NewDecoder(v.reader).Decode(&response); err != nil {
		v.closeConn()
		return nil, "", err
	}
	if response.Error != "" {
		if response.FailureCode != "" {
			return nil, "", fmt.Errorf("vision face embedding failed code=%s: %s", response.FailureCode, response.Error)
		}
		return nil, "", fmt.Errorf("vision face embedding failed: %s", response.Error)
	}
	if response.RequestID != requestID {
		return nil, "", fmt.Errorf("vision face embedding response correlation mismatch")
	}
	return append([]float32(nil), response.Embedding...), response.ModelFingerprint, nil
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
	return NewRuntimeWithManagerAndSocketTimeout(manager, SocketPath, WorkerTimeout)
}

func NewRuntimeWithManagerAndSocket(
	manager *WorkerManager,
	socketPath string,
) *Runtime {
	return NewRuntimeWithManagerAndSocketTimeout(manager, socketPath, WorkerTimeout)
}

func NewRuntimeWithManagerAndSocketTimeout(
	manager *WorkerManager,
	socketPath string,
	workerTimeout time.Duration,
) *Runtime {
	if strings.TrimSpace(socketPath) == "" {
		socketPath = SocketPath
	}
	if workerTimeout <= 0 {
		workerTimeout = WorkerTimeout
	}
	return &Runtime{
		manager:       manager,
		socketPath:    socketPath,
		workerTimeout: workerTimeout,
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
			v.socketPath,
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

// Close releases the persistent worker connection. Worker process ownership
// remains with WorkerManager so callers can stop it explicitly and observe
// the bounded shutdown result.
func (v *Runtime) Close() error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	v.closeConn()
	v.mu.Unlock()
	return nil
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
	conn := v.conn
	if err := conn.SetDeadline(time.Now().UTC().Add(v.workerTimeout)); err != nil {
		v.closeConn()
		return nil, fmt.Errorf("set vision worker deadline: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	req := Request{
		ID:           job.ID,
		ActivationID: job.ActivationID,
		ClipIndex:    job.ClipIndex,
		NodeID:       job.NodeID,
		SequenceKey:  job.SequenceKey,
		TrackID:      job.TrackID,

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
