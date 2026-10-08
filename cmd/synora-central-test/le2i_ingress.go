package main

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
	"strings"
	"sync"
	"time"

	"synora/internal/discovery/ingress"
	"synora/internal/discovery/vision"
	"synora/pkg/contract"
)

type le2iIngressQueue struct {
	mu  sync.Mutex
	job *vision.ClipJob
}

func (q *le2iIngressQueue) Enqueue(job *vision.ClipJob) error {
	if job == nil || job.Path == "" || job.SimulatedCamera {
		return errors.New("Le2i ingress queue rejected invalid media metadata")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.job != nil {
		return errors.New("Le2i ingress queue already has a job")
	}
	copy := *job
	q.job = &copy
	return nil
}

type le2iLifecycleCollector struct {
	mu       sync.Mutex
	root     string
	messages []contract.Message
	err      error
}

func (p *le2iLifecycleCollector) Send(message contract.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = message
	p.err = errors.New("clip lifecycle must remain private to Discovery")
	return p.err
}

func ingressLe2iClip(sourcePath, clipID string) (storedPath string, status, lifecycleCount int, cleanup func(), err error) {
	root, err := os.MkdirTemp("", "synora-le2i-ingress-")
	if err != nil {
		return "", 0, 0, func() {}, errors.New("temporary Le2i ingress storage unavailable")
	}
	cleanup = func() { _ = os.RemoveAll(root) }
	info, err := os.Stat(sourcePath)
	if err != nil || !info.Mode().IsRegular() {
		cleanup()
		return "", 0, 0, func() {}, errors.New("Le2i media file unavailable for ingress")
	}
	queue := &le2iIngressQueue{}
	publisher := &le2iLifecycleCollector{root: root}
	handler := ingress.NewHandler(ingress.Config{
		ClipDir: filepath.Join(root, "clips"), Queue: queue, Publisher: publisher,
		MaxClipSize: info.Size() + 1, MaxClipCount: 1, MaxClipBytes: info.Size() + 1,
		MinFreeBytes: 1, MaxClipDuration: time.Minute, ClipDuration: time.Second,
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	part, err := multipartWriter.CreateFormFile("clip", "clip.mp4")
	if err == nil {
		var file *os.File
		file, err = os.Open(sourcePath)
		if err == nil {
			_, err = io.Copy(part, file)
			_ = file.Close()
		}
	}
	if err == nil {
		err = multipartWriter.WriteField("clip_id", clipID)
	}
	if err == nil {
		err = multipartWriter.WriteField("simulated_camera", "false")
	}
	if closeErr := multipartWriter.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", 0, 0, func() {}, errors.New("Le2i HTTP upload could not be prepared")
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/vision", &body)
	if err != nil {
		cleanup()
		return "", 0, 0, func() {}, errors.New("Le2i HTTP request could not be prepared")
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("X-Synora-Device", "cam_le2i_test")
	request.Header.Set("X-Synora-Clip-ID", clipID)
	request.Header.Set("X-Synora-Simulated-Camera", "false")
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		cleanup()
		return "", 0, 0, func() {}, errors.New("Le2i Discovery ingress request failed")
	}
	defer response.Body.Close()
	status = response.StatusCode
	if status != http.StatusAccepted {
		cleanup()
		return "", status, 0, func() {}, errors.New("Le2i Discovery ingress rejected the clip")
	}
	var ack struct {
		Status string `json:"status"`
		ClipID string `json:"clip_id"`
	}
	if json.NewDecoder(response.Body).Decode(&ack) != nil || ack.Status != "queued" || ack.ClipID != clipID {
		cleanup()
		return "", status, 0, func() {}, errors.New("Le2i Discovery ingress acknowledgement was invalid")
	}
	queue.mu.Lock()
	job := queue.job
	queue.mu.Unlock()
	publisher.mu.Lock()
	lifecycleCount = len(publisher.messages)
	publicationErr := publisher.err
	publisher.mu.Unlock()
	if job == nil || publicationErr != nil || lifecycleCount != 0 || !strings.HasPrefix(job.Path, root+string(filepath.Separator)) {
		cleanup()
		return "", status, lifecycleCount, func() {}, errors.New("Le2i ingress queue was incomplete or clip lifecycle escaped Discovery")
	}
	storedPath = job.Path
	return storedPath, status, lifecycleCount, cleanup, nil
}
