package ingress

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"synora/internal/clipstore"
	"synora/internal/discovery/vision"
	"synora/internal/idgen"
	"synora/internal/retention"
	"synora/pkg/contract"
)

type Authenticator interface {
	VerifyCameraRequest(r *http.Request, bodyHash string) error
}

type DeviceTracker interface {
	TouchCameraClip(deviceID string, now time.Time) bool
}

type Queue interface {
	Enqueue(job *vision.ClipJob) error
}

type Publisher interface {
	Send(msg contract.Message) error
}

type Config struct {
	Addr string

	CertFile string
	KeyFile  string

	ClipDir      string
	MaxClipSize  int64
	MaxClipCount int
	MaxClipBytes int64
	MinFreeBytes int64
	TempMaxAge   time.Duration

	Authenticator Authenticator
	Devices       DeviceTracker
	Queue         Queue
	Publisher     Publisher

	AllowInsecure bool
	OnStatus      func(status, reason string)
}

const defaultTempMaxAge = time.Hour

// NewHandler exposes the real upload handler for integration tests and keeps
// the physical storage path independent from the HTTP server lifecycle.
func NewHandler(cfg Config) http.Handler {
	if cfg.MaxClipSize <= 0 {
		cfg.MaxClipSize = 50 << 20
	}
	if cfg.TempMaxAge <= 0 {
		cfg.TempMaxAge = defaultTempMaxAge
	}
	if cfg.MaxClipCount <= 0 {
		cfg.MaxClipCount = 500
	}
	if cfg.MaxClipBytes <= 0 {
		cfg.MaxClipBytes = 5 << 30
	}
	if cfg.MinFreeBytes <= 0 {
		cfg.MinFreeBytes = retention.DefaultPolicy().MinFreeBytes
	}
	_ = ReconcileStorage(cfg.ClipDir, cfg.TempMaxAge)
	var uploadMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/vision", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		deviceID := strings.TrimSpace(r.Header.Get("X-Synora-Device"))
		if !clipstore.SafeComponent(deviceID) {
			http.Error(w, "valid device required", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxClipSize)
		file, header, err := r.FormFile("clip")
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(strings.ToLower(err.Error()), "too large") {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, "clip required", status)
			return
		}
		defer file.Close()

		clipID := firstNonEmpty(r.Header.Get("X-Synora-Clip-ID"), r.FormValue("clip_id"))
		if clipID == "" {
			clipID = idgen.New("clip")
		}
		if !clipstore.SafeComponent(clipID) {
			http.Error(w, "invalid clip id", http.StatusBadRequest)
			return
		}
		clipIndex, err := parseOptionalInt(firstNonEmpty(r.Header.Get("X-Synora-Clip-Index"), r.FormValue("clip_index")))
		if err != nil {
			http.Error(w, "invalid clip index", http.StatusBadRequest)
			return
		}
		activationID := firstNonEmpty(r.Header.Get("X-Synora-Activation-ID"), r.FormValue("activation_id"))
		sequenceKey := firstNonEmpty(r.Header.Get("X-Synora-Sequence-Key"), r.FormValue("sequence_key"))
		trackID := firstNonEmpty(r.Header.Get("X-Synora-Track-ID"), r.FormValue("track_id"))
		nodeID := firstNonEmpty(r.Header.Get("X-Synora-Node-ID"), r.FormValue("node_id"))

		uploadMu.Lock()
		defer uploadMu.Unlock()
		now := time.Now().UTC()
		finalPath, err := clipstore.FinalPath(cfg.ClipDir, deviceID, clipID)
		if err != nil {
			http.Error(w, "invalid clip path", http.StatusBadRequest)
			return
		}
		partPath, err := clipstore.PartPath(cfg.ClipDir, deviceID, clipID)
		if err != nil {
			http.Error(w, "invalid clip path", http.StatusBadRequest)
			return
		}
		if _, err := clipstore.EnsureCameraDir(cfg.ClipDir, deviceID); err != nil {
			http.Error(w, "clip storage unavailable", http.StatusInsufficientStorage)
			return
		}
		if info, err := os.Lstat(partPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			http.Error(w, "unsafe clip temporary path", http.StatusConflict)
			return
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "clip temporary storage unavailable", http.StatusInsufficientStorage)
			return
		}

		part, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			http.Error(w, "clip temporary storage unavailable", http.StatusInsufficientStorage)
			return
		}
		hash := sha256.New()
		size, copyErr := io.Copy(io.MultiWriter(part, hash), file)
		syncErr := part.Sync()
		closeErr := part.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil {
			_ = os.Remove(partPath)
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(fmt.Sprint(copyErr)), "too large") {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, "clip write failed", status)
			return
		}
		if size <= 0 || size > cfg.MaxClipSize {
			_ = os.Remove(partPath)
			status := http.StatusBadRequest
			if size > cfg.MaxClipSize {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, "empty or oversized clip", status)
			return
		}
		checksum := hex.EncodeToString(hash.Sum(nil))
		if cfg.Authenticator != nil {
			if err := cfg.Authenticator.VerifyCameraRequest(r, checksum); err != nil {
				_ = os.Remove(partPath)
				log.Printf("ingress auth failed device=%s err=%v", deviceID, err)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		finalizedHere := false
		if info, statErr := os.Lstat(finalPath); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				_ = os.Remove(partPath)
				http.Error(w, "unsafe existing clip path", http.StatusConflict)
				return
			}
			existingSize, existingChecksum, hashErr := fileDigest(finalPath)
			if hashErr != nil || existingSize != size || existingChecksum != checksum {
				_ = os.Remove(partPath)
				http.Error(w, "clip id content collision", http.StatusConflict)
				return
			}
			_ = os.Remove(partPath)
		} else if errors.Is(statErr, os.ErrNotExist) {
			available, err := storageCapacityAvailable(cfg.ClipDir, cfg.MaxClipCount, cfg.MaxClipBytes, size, cfg.MinFreeBytes)
			if err != nil {
				_ = os.Remove(partPath)
				http.Error(w, "clip storage unavailable", http.StatusInsufficientStorage)
				return
			}
			if !available {
				_ = os.Remove(partPath)
				http.Error(w, "clip storage quota exhausted", http.StatusInsufficientStorage)
				return
			}
			if err := os.Rename(partPath, finalPath); err != nil {
				_ = os.Remove(partPath)
				http.Error(w, "clip finalization failed", http.StatusInsufficientStorage)
				return
			}
			finalizedHere = true
			if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
				_ = os.Remove(finalPath)
				http.Error(w, "clip finalization durability failed", http.StatusInsufficientStorage)
				return
			}
		} else {
			_ = os.Remove(partPath)
			http.Error(w, "clip path unavailable", http.StatusInsufficientStorage)
			return
		}
		if valid, verifyErr := clipstore.VerifyRegularFile(finalPath, size, checksum); verifyErr != nil || !valid {
			if finalizedHere {
				_ = os.Remove(finalPath)
			}
			http.Error(w, "clip finalization verification failed", http.StatusInsufficientStorage)
			return
		}
		if cfg.Publisher == nil {
			if finalizedHere {
				_ = os.Remove(finalPath)
			}
			http.Error(w, "core unavailable", http.StatusServiceUnavailable)
			return
		}

		mediaType := strings.TrimSpace(header.Header.Get("Content-Type"))
		clip := contract.Clip{
			ID: clipID, ActivationID: activationID, ClipIndex: clipIndex,
			SequenceKey: sequenceKey, TrackID: trackID,
			CameraID: deviceID, NodeID: nodeID, CreatedAt: now, ReceivedAt: now,
			ReadyAt: now, Status: contract.ClipStatusReady, SizeBytes: size,
			Checksum: checksum, MediaType: mediaType, Container: "mp4",
			UpdatedAt: now, Revision: 1,
		}
		if cfg.Devices != nil {
			cfg.Devices.TouchCameraClip(deviceID, now)
		}
		if err := publishLifecycle(cfg.Publisher, contract.EventClipReady, clip, "", clipID+":ready"); err != nil {
			if finalizedHere {
				_ = os.Remove(finalPath)
			}
			log.Printf("clip ready publication failed clip=%s err=%v", clipID, err)
			http.Error(w, "core unavailable", http.StatusServiceUnavailable)
			return
		}
		if cfg.Queue == nil {
			_ = publishLifecycle(cfg.Publisher, contract.EventClipFailed, clip, "analysis_queue_unavailable", clipID+":failed")
			http.Error(w, "analysis queue unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := cfg.Queue.Enqueue(&vision.ClipJob{ID: clipID, CameraID: deviceID, Path: finalPath, CreatedAt: now, ActivationID: activationID, ClipIndex: clipIndex, NodeID: nodeID, SequenceKey: sequenceKey, TrackID: trackID}); err != nil {
			log.Printf("analysis queue unavailable clip=%s err=%v", clipID, err)
			_ = publishLifecycle(cfg.Publisher, contract.EventClipFailed, clip, "analysis_queue_full", clipID+":failed")
			http.Error(w, "analysis queue full", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "queued", "clip_id": clipID})
	})
	return mux
}

func StartServer(cfg Config) {
	certMissing := !regularFile(cfg.CertFile)
	keyMissing := !regularFile(cfg.KeyFile)
	if certMissing || keyMissing {
		reason := "tls_cert_missing"
		if !certMissing && keyMissing {
			reason = "tls_key_missing"
		}
		if !cfg.AllowInsecure {
			setStatus(cfg, "disabled", reason)
			log.Printf("vision ingress disabled status=disabled reason=%s cert=%s key=%s", reason, cfg.CertFile, cfg.KeyFile)
			return
		}
		log.Printf("vision ingress insecure fallback enabled reason=%s", reason)
	}
	handler := NewHandler(cfg)
	server := &http.Server{Addr: cfg.Addr, Handler: handler, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		listener, err := net.Listen("tcp", cfg.Addr)
		if err != nil {
			setStatus(cfg, "error", err.Error())
			return
		}
		if certMissing || keyMissing {
			setStatus(cfg, "degraded", "listening insecure local mode")
			err = server.Serve(listener)
		} else {
			certificate, loadErr := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
			if loadErr != nil {
				_ = listener.Close()
				setStatus(cfg, "error", loadErr.Error())
				return
			}
			setStatus(cfg, "ok", "listening")
			server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
			err = server.ServeTLS(listener, "", "")
		}
		if err != nil && err != http.ErrServerClosed {
			setStatus(cfg, "error", err.Error())
		}
	}()
}

func publishLifecycle(publisher Publisher, eventType string, clip contract.Clip, failureCode, eventID string) error {
	if publisher == nil {
		return nil
	}
	if failureCode != "" {
		clip.FailureCode = failureCode
		clip.Status = contract.ClipStatusFailed
	}
	body, err := json.Marshal(contract.ClipLifecyclePayload{Clip: clip, ClipID: clip.ID, CameraID: clip.CameraID, FailureCode: failureCode})
	if err != nil {
		return err
	}
	return publisher.Send(contract.Message{ID: eventID, Type: eventType, Kind: contract.KindEvent, Source: "discovery", Target: "core", Timestamp: time.Now().UTC(), Payload: body})
}

func fileDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func storageCapacityAvailable(root string, maxCount int, maxBytes, incomingBytes, minFreeBytes int64) (bool, error) {
	if maxCount <= 0 || maxBytes <= 0 || incomingBytes < 0 || minFreeBytes <= 0 {
		return false, errors.New("invalid clip storage quota")
	}
	count := 0
	var totalBytes int64
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return incomingBytes <= maxBytes, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, errors.New("clip storage root is not a directory")
	}
	if reserve, reserveErr := retention.HasReserve(root, incomingBytes, minFreeBytes); reserveErr != nil || !reserve {
		return false, reserveErr
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".mp4") {
			return nil
		}
		entryInfo, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		count++
		totalBytes += entryInfo.Size()
		return nil
	})
	if err != nil {
		return false, err
	}
	return count < maxCount && totalBytes <= maxBytes-incomingBytes, nil
}

func ReconcileStorage(root string, maxPartAge time.Duration) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	if maxPartAge <= 0 {
		maxPartAge = defaultTempMaxAge
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return err
	}
	now := time.Now().UTC()
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".part") {
			if strings.HasSuffix(entry.Name(), ".mp4") {
				log.Printf("clip orphan candidate")
			}
			return nil
		}
		stat, statErr := entry.Info()
		if statErr == nil && now.Sub(stat.ModTime().UTC()) > maxPartAge {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return removeErr
			}
		}
		return nil
	})
}

func parseOptionalInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return strconv.Atoi(strings.TrimSpace(raw))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func setStatus(cfg Config, status, reason string) {
	if cfg.OnStatus != nil {
		cfg.OnStatus(status, reason)
	}
}
