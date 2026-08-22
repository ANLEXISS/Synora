package ingress

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/discovery/vision"
	"synora/pkg/contract"
)

type clipTestQueue struct{ jobs []*vision.ClipJob }

func (q *clipTestQueue) Enqueue(job *vision.ClipJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}

type clipTestPublisher struct {
	root     string
	messages []contract.Message
}

func (p *clipTestPublisher) Send(message contract.Message) error {
	if message.Type == contract.EventClipReady {
		var payload contract.ClipLifecyclePayload
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			return err
		}
		path := filepath.Join(p.root, payload.CameraID, payload.ClipID+".mp4")
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	p.messages = append(p.messages, message)
	return nil
}

func TestClipIngressFinalizesBeforeReadyAndQueuesMetadata(t *testing.T) {
	root := t.TempDir()
	queue := &clipTestQueue{}
	publisher := &clipTestPublisher{root: root}
	handler := NewHandler(Config{ClipDir: root, Queue: queue, Publisher: publisher, MaxClipSize: 1024})

	recorder, contentType := multipartRequest(t, "cam-1", "clip-1", []byte("video-bytes"))
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "clip-1")
	recorder.Header.Set("X-Synora-Activation-ID", "activation-1")
	recorder.Header.Set("X-Synora-Sequence-Key", "sequence-1")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	finalPath := filepath.Join(root, "cam-1", "clip-1.mp4")
	if data, err := os.ReadFile(finalPath); err != nil || string(data) != "video-bytes" {
		t.Fatalf("final clip mismatch data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", ".clip-1.part")); !os.IsNotExist(err) {
		t.Fatalf("temporary part should not remain: %v", err)
	}
	if len(queue.jobs) != 1 || queue.jobs[0].Path != finalPath {
		t.Fatalf("unexpected queued job: %#v", queue.jobs)
	}
	if len(publisher.messages) != 1 || publisher.messages[0].Type != contract.EventClipReady {
		t.Fatalf("ready must be published once after finalization: %#v", publisher.messages)
	}
}

func TestClipIngressRetryIsIdempotentAndCollisionRejected(t *testing.T) {
	root := t.TempDir()
	queue := &clipTestQueue{}
	publisher := &clipTestPublisher{root: root}
	handler := NewHandler(Config{ClipDir: root, Queue: queue, Publisher: publisher, MaxClipSize: 1024})

	for _, data := range [][]byte{[]byte("same"), []byte("same")} {
		recorder, contentType := multipartRequest(t, "cam-1", "clip-1", data)
		recorder.Header.Set("X-Synora-Device", "cam-1")
		recorder.Header.Set("X-Synora-Clip-ID", "clip-1")
		recorder.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, recorder)
		if response.Code != http.StatusAccepted {
			t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if len(queue.jobs) != 2 || len(publisher.messages) != 2 {
		t.Fatalf("same retry must remain at-least-once with stable identity: jobs=%d messages=%d", len(queue.jobs), len(publisher.messages))
	}
	if queue.jobs[0].ID != queue.jobs[1].ID || publisher.messages[0].ID != publisher.messages[1].ID {
		t.Fatalf("same retry changed stable identity: jobs=%#v messages=%#v", queue.jobs, publisher.messages)
	}

	recorder, contentType := multipartRequest(t, "cam-1", "clip-1", []byte("different"))
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "clip-1")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusConflict {
		t.Fatalf("collision status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestClipIngressRejectsWhenPhysicalQuotaIsExhausted(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cam-1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cam-1", "existing.mp4"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(Config{ClipDir: root, Queue: &clipTestQueue{}, Publisher: &clipTestPublisher{root: root}, MaxClipSize: 1024, MaxClipCount: 1, MaxClipBytes: 1024})
	recorder, contentType := multipartRequest(t, "cam-1", "clip-2", []byte("new"))
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "clip-2")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusInsufficientStorage {
		t.Fatalf("quota status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", ".clip-2.part")); !os.IsNotExist(err) {
		t.Fatalf("quota rejection must remove part file: %v", err)
	}
}

func TestClipIngressRejectsUnsafeNamesAndSize(t *testing.T) {
	root := t.TempDir()
	handler := NewHandler(Config{ClipDir: root, Queue: &clipTestQueue{}, MaxClipSize: 4})

	recorder, contentType := multipartRequest(t, "../escape", "clip-1", []byte("ok"))
	recorder.Header.Set("X-Synora-Device", "../escape")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsafe camera status=%d", response.Code)
	}

	recorder, contentType = multipartRequest(t, "cam-1", "clip-2", []byte("too-large"))
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "clip-2")
	recorder.Header.Set("Content-Type", contentType)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestClipIngressRejectsDangerousSymlinkAndCleansOldParts(t *testing.T) {
	root := t.TempDir()
	cameraDir := filepath.Join(root, "cam-1")
	if err := os.MkdirAll(cameraDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(cameraDir, "clip-1.mp4")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	part := filepath.Join(cameraDir, ".old.part")
	if err := os.WriteFile(part, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(part, old, old); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStorage(root, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("old part should be cleaned: %v", err)
	}

	handler := NewHandler(Config{ClipDir: root, Queue: &clipTestQueue{}})
	recorder, contentType := multipartRequest(t, "cam-1", "clip-1", []byte("same"))
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "clip-1")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusConflict {
		t.Fatalf("symlink status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestClipIngressRejectsEmptyPayload(t *testing.T) {
	root := t.TempDir()
	handler := NewHandler(Config{ClipDir: root, Queue: &clipTestQueue{}, Publisher: &clipTestPublisher{root: root}})
	recorder, contentType := multipartRequest(t, "cam-1", "empty", nil)
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "empty")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty payload status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", ".empty.part")); !os.IsNotExist(err) {
		t.Fatalf("empty payload left temporary file: %v", err)
	}
}

func TestClipIngressCleansPartialUpload(t *testing.T) {
	root := t.TempDir()
	handler := NewHandler(Config{ClipDir: root, Queue: &clipTestQueue{}, Publisher: &clipTestPublisher{root: root}})
	recorder, contentType := multipartRequest(t, "cam-1", "partial", []byte("partial-data"))
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = recorder.Body.Close()
	recorder.Body = io.NopCloser(&failingReader{data: body[:len(body)/2], err: errors.New("client disconnected")})
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "partial")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusBadRequest && response.Code != http.StatusInternalServerError {
		t.Fatalf("partial upload status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", ".partial.part")); !os.IsNotExist(err) {
		t.Fatalf("partial upload left temporary file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", "partial.mp4")); !os.IsNotExist(err) {
		t.Fatalf("partial upload left final file: %v", err)
	}
}

func TestClipIngressRemovesNewFinalWhenCorePublicationFails(t *testing.T) {
	root := t.TempDir()
	handler := NewHandler(Config{
		ClipDir:   root,
		Queue:     &clipTestQueue{},
		Publisher: failingPublisher{err: errors.New("core unavailable")},
	})
	recorder, contentType := multipartRequest(t, "cam-1", "publish-failure", []byte("clip"))
	recorder.Header.Set("X-Synora-Device", "cam-1")
	recorder.Header.Set("X-Synora-Clip-ID", "publish-failure")
	recorder.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, recorder)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("publication failure status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "cam-1", "publish-failure.mp4")); !os.IsNotExist(err) {
		t.Fatalf("failed publication left final file: %v", err)
	}
}

type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

type failingPublisher struct{ err error }

func (p failingPublisher) Send(contract.Message) error { return p.err }

func multipartRequest(t *testing.T, cameraID, clipID string, data []byte) (*http.Request, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("clip", clipID+".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	_ = writer.WriteField("camera_id", cameraID)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/vision", &body)
	return req, writer.FormDataContentType()
}
