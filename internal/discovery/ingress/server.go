package ingress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"synora/internal/discovery/vision"
	"synora/internal/idgen"
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

type Config struct {
	Addr string

	CertFile string

	KeyFile string

	ClipDir string

	MaxClipSize int64

	Authenticator Authenticator

	Devices DeviceTracker

	Queue Queue

	AllowInsecure bool
	OnStatus      func(status, reason string)
}

func StartServer(
	cfg Config,
) {
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
		setStatus(cfg, "degraded", reason)
		log.Printf("vision ingress insecure fallback enabled reason=%s", reason)
	} else {
		setStatus(cfg, "ok", "")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/vision", func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if r.Method != http.MethodPost {

			w.WriteHeader(
				http.StatusMethodNotAllowed,
			)

			return
		}

		r.Body = http.MaxBytesReader(
			w,
			r.Body,
			cfg.MaxClipSize,
		)

		deviceID := r.Header.Get(
			"X-Synora-Device",
		)

		file, _, err := r.FormFile(
			"clip",
		)

		if deviceID == "" || err != nil {

			http.Error(
				w,
				"device and clip required",
				http.StatusBadRequest,
			)

			return
		}

		defer file.Close()

		now := time.Now().UTC()

		clipID := idgen.New(
			"clip",
		)

		tmpFile, err := os.CreateTemp(
			"",
			"synora-upload-*",
		)

		if err != nil {

			http.Error(
				w,
				"temp file error",
				http.StatusInternalServerError,
			)

			return
		}

		tmpPath := tmpFile.Name()

		defer func() {

			if tmpPath != "" {

				os.Remove(
					tmpPath,
				)
			}
		}()

		defer tmpFile.Close()

		hash := sha256.New()

		writer := io.MultiWriter(
			tmpFile,
			hash,
		)

		size, err := io.Copy(
			writer,
			file,
		)

		if err != nil {

			http.Error(
				w,
				"copy error",
				http.StatusInternalServerError,
			)

			return
		}

		bodyHash := hex.EncodeToString(
			hash.Sum(nil),
		)

		err = cfg.Authenticator.VerifyCameraRequest(
			r,
			bodyHash,
		)

		if err != nil {

			log.Printf(
				"ingress auth failed device=%s err=%v",
				deviceID,
				err,
			)

			http.Error(
				w,
				"unauthorized",
				http.StatusUnauthorized,
			)

			return
		}

		deviceDir := filepath.Join(
			cfg.ClipDir,
			deviceID,
		)

		err = os.MkdirAll(
			deviceDir,
			0755,
		)

		if err != nil {

			http.Error(
				w,
				"directory error",
				http.StatusInternalServerError,
			)

			return
		}

		clipPath := filepath.Join(
			deviceDir,
			fmt.Sprintf(
				"%s.mp4",
				clipID,
			),
		)

		err = os.Rename(
			tmpPath,
			clipPath,
		)

		if err != nil {

			http.Error(
				w,
				"persist error",
				http.StatusInternalServerError,
			)

			return
		}

		tmpPath = ""

		cfg.Devices.TouchCameraClip(
			deviceID,
			now,
		)

		err = cfg.Queue.Enqueue(
			&vision.ClipJob{
				ID: clipID,

				CameraID: deviceID,

				Path: clipPath,

				CreatedAt: now,
			},
		)

		if err != nil {

			log.Printf(
				"queue full clip=%s device=%s",
				clipID,
				deviceID,
			)

			os.Remove(
				clipPath,
			)

			http.Error(
				w,
				"analysis queue full",
				http.StatusServiceUnavailable,
			)

			return
		}

		log.Printf(
			"clip accepted device=%s clip=%s size=%d",
			deviceID,
			clipID,
			size,
		)

		w.Header().Set(
			"Content-Type",
			"application/json",
		)

		err = json.NewEncoder(w).Encode(
			map[string]any{
				"status":  "queued",
				"clip_id": clipID,
			},
		)

		if err != nil {

			log.Printf(
				"response encode failed clip=%s err=%v",
				clipID,
				err,
			)
		}
	})

	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {

		log.Printf(
			"vision ingress listening addr=%s",
			cfg.Addr,
		)

		var err error
		if certMissing || keyMissing {
			err = server.ListenAndServe()
		} else {
			err = server.ListenAndServeTLS(
				cfg.CertFile,
				cfg.KeyFile,
			)
		}

		if err != nil && err != http.ErrServerClosed {
			setStatus(cfg, "error", err.Error())

			log.Printf(
				"vision ingress stopped err=%v",
				err,
			)
		}
	}()
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
