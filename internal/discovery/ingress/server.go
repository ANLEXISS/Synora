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
	"math"
	"mime"
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

	ClipDir         string
	MaxClipSize     int64
	MaxClipDuration time.Duration
	MaxClipCount    int
	MaxClipBytes    int64
	MinFreeBytes    int64
	TempMaxAge      time.Duration

	Authenticator Authenticator
	Devices       DeviceTracker
	Queue         Queue
	Publisher     Publisher

	AllowInsecure bool
	OnStatus      func(status, reason string)
}

const defaultTempMaxAge = time.Hour

const (
	defaultMaxClipDuration = 60 * time.Second
	multipartOverheadLimit = 1 << 20
	multipartFieldLimit    = 64 << 10
)

var (
	errUploadTooLarge    = errors.New("clip upload too large")
	errUploadIncomplete  = errors.New("clip upload incomplete")
	errUploadStorage     = errors.New("clip upload storage unavailable")
	errUnsupportedMedia  = errors.New("unsupported clip media")
	errDuplicateClipPart = errors.New("duplicate clip part")
)

// NewHandler exposes the real upload handler for integration tests and keeps
// the physical storage path independent from the HTTP server lifecycle.
func NewHandler(cfg Config) http.Handler {
	if cfg.MaxClipSize <= 0 {
		cfg.MaxClipSize = 50 << 20
	}
	if cfg.MaxClipDuration <= 0 {
		cfg.MaxClipDuration = defaultMaxClipDuration
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
		bodyLimit := cfg.MaxClipSize + multipartOverheadLimit
		if bodyLimit < cfg.MaxClipSize {
			bodyLimit = cfg.MaxClipSize
		}
		r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
		headerClipID := firstNonEmpty(r.Header.Get("X-Synora-Clip-ID"))
		if headerClipID != "" && !clipstore.SafeComponent(headerClipID) {
			http.Error(w, "invalid clip id", http.StatusBadRequest)
			return
		}

		uploadMu.Lock()
		defer uploadMu.Unlock()
		cameraDir, err := clipstore.EnsureCameraDir(cfg.ClipDir, deviceID)
		if err != nil {
			http.Error(w, "clip storage unavailable", http.StatusInsufficientStorage)
			return
		}
		upload, err := receiveMultipartUpload(r, cameraDir, cfg.MaxClipSize)
		if err != nil {
			status := http.StatusBadRequest
			message := "invalid clip upload"
			if errors.Is(err, errUploadTooLarge) {
				status = http.StatusRequestEntityTooLarge
				message = "clip too large"
			} else if errors.Is(err, errUploadIncomplete) {
				message = "incomplete clip upload"
			} else if errors.Is(err, errUnsupportedMedia) {
				status = http.StatusUnsupportedMediaType
				message = "unsupported clip media"
			} else if errors.Is(err, errUploadStorage) {
				status = http.StatusInsufficientStorage
				message = "clip storage unavailable"
			}
			http.Error(w, message, status)
			return
		}
		defer os.Remove(upload.tempPath)

		clipID := firstNonEmpty(headerClipID, upload.fields["clip_id"])
		if clipID == "" {
			clipID = idgen.New("clip")
		}
		if !clipstore.SafeComponent(clipID) {
			http.Error(w, "invalid clip id", http.StatusBadRequest)
			return
		}
		clipIndex, err := parseOptionalInt(firstNonEmpty(r.Header.Get("X-Synora-Clip-Index"), upload.fields["clip_index"]))
		if err != nil || clipIndex < 0 {
			http.Error(w, "invalid clip index", http.StatusBadRequest)
			return
		}
		activationID := firstNonEmpty(r.Header.Get("X-Synora-Activation-ID"), upload.fields["activation_id"])
		sequenceKey := firstNonEmpty(r.Header.Get("X-Synora-Sequence-Key"), upload.fields["sequence_key"])
		trackID := firstNonEmpty(r.Header.Get("X-Synora-Track-ID"), upload.fields["track_id"])
		nodeID := firstNonEmpty(r.Header.Get("X-Synora-Node-ID"), upload.fields["node_id"])
		container := strings.ToLower(firstNonEmpty(r.Header.Get("X-Synora-Clip-Container"), upload.fields["container"]))
		if container != "" && container != "mp4" {
			http.Error(w, "unsupported clip container", http.StatusUnsupportedMediaType)
			return
		}
		duration, err := parseClipDuration(firstNonEmpty(r.Header.Get("X-Synora-Clip-Duration"), upload.fields["duration"]))
		if err != nil || duration > cfg.MaxClipDuration.Seconds() {
			http.Error(w, "clip duration exceeds limit", http.StatusRequestEntityTooLarge)
			return
		}
		expectedChecksum := firstNonEmpty(r.Header.Get("X-Synora-Clip-Checksum"), upload.fields["checksum"])
		if expectedChecksum != "" && (!isSHA256Hex(expectedChecksum) || !strings.EqualFold(expectedChecksum, upload.checksum)) {
			http.Error(w, "clip checksum mismatch", http.StatusBadRequest)
			return
		}
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
		if info, err := os.Lstat(partPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			http.Error(w, "unsafe clip temporary path", http.StatusConflict)
			return
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "clip temporary storage unavailable", http.StatusInsufficientStorage)
			return
		}

		if err := os.Rename(upload.tempPath, partPath); err != nil {
			_ = os.Remove(partPath)
			http.Error(w, "clip temporary storage unavailable", http.StatusInsufficientStorage)
			return
		}
		size, checksum := upload.size, upload.checksum
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

		clip := contract.Clip{
			ID: clipID, ActivationID: activationID, ClipIndex: clipIndex,
			SequenceKey: sequenceKey, TrackID: trackID,
			CameraID: deviceID, NodeID: nodeID, CreatedAt: now, ReceivedAt: now,
			ReadyAt: now, Status: contract.ClipStatusReady, SizeBytes: size,
			Checksum: checksum, MediaType: upload.mediaType, Container: "mp4", Duration: duration,
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

func StartServer(cfg Config) *http.Server {
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
			return nil
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
	return server
}

type receivedUpload struct {
	tempPath  string
	fields    map[string]string
	filename  string
	mediaType string
	size      int64
	checksum  string
}

func receiveMultipartUpload(r *http.Request, cameraDir string, maxSize int64) (receivedUpload, error) {
	if r == nil || r.Body == nil || maxSize <= 0 {
		return receivedUpload{}, errUploadIncomplete
	}
	temp, err := os.CreateTemp(cameraDir, ".upload-*.part")
	if err != nil {
		return receivedUpload{}, fmt.Errorf("%w: %v", errUploadStorage, err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()

	multipartReader, err := r.MultipartReader()
	if err != nil {
		return receivedUpload{}, errUploadIncomplete
	}
	fields := make(map[string]string)
	result := receivedUpload{tempPath: tempPath, fields: fields}
	clipSeen := false
	for {
		part, nextErr := multipartReader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return receivedUpload{}, errUploadIncomplete
		}
		name := strings.TrimSpace(part.FormName())
		if name == "clip" {
			if clipSeen {
				return receivedUpload{}, errDuplicateClipPart
			}
			clipSeen = true
			filename := strings.TrimSpace(part.FileName())
			if filename == "" || filepath.Base(filename) != filename || strings.ToLower(filepath.Ext(filename)) != ".mp4" {
				return receivedUpload{}, errUnsupportedMedia
			}
			mediaType := strings.TrimSpace(part.Header.Get("Content-Type"))
			if !supportedClipMediaType(mediaType) {
				return receivedUpload{}, errUnsupportedMedia
			}
			hash := sha256.New()
			size, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(part, maxSize+1))
			if copyErr != nil {
				return receivedUpload{}, errUploadIncomplete
			}
			if size > maxSize {
				return receivedUpload{}, errUploadTooLarge
			}
			if size == 0 {
				return receivedUpload{}, errUploadIncomplete
			}
			result.filename = filename
			result.mediaType = mediaType
			result.size = size
			result.checksum = hex.EncodeToString(hash.Sum(nil))
			continue
		}
		if name == "" {
			return receivedUpload{}, errUploadIncomplete
		}
		value, readErr := io.ReadAll(io.LimitReader(part, multipartFieldLimit+1))
		if readErr != nil {
			return receivedUpload{}, errUploadIncomplete
		}
		if int64(len(value)) > multipartFieldLimit {
			return receivedUpload{}, errUploadTooLarge
		}
		fields[name] = strings.TrimSpace(string(value))
	}
	if !clipSeen {
		return receivedUpload{}, errUploadIncomplete
	}
	if err := temp.Sync(); err != nil {
		return receivedUpload{}, fmt.Errorf("%w: %v", errUploadStorage, err)
	}
	if err := temp.Close(); err != nil {
		return receivedUpload{}, fmt.Errorf("%w: %v", errUploadStorage, err)
	}
	keep = true
	return result, nil
}

func supportedClipMediaType(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "video/mp4", "application/mp4", "application/octet-stream":
		return true
	default:
		return false
	}
}

func parseClipDuration(raw string) (float64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0, errors.New("invalid clip duration")
	}
	return duration, nil
}

func isSHA256Hex(raw string) bool {
	if len(strings.TrimSpace(raw)) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimSpace(raw))
	return err == nil
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
