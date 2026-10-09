package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"synora/internal/bus"
)

const residentGalleryRPC = "core.resident_gallery"

type busResidentGalleryService struct{ client *bus.Client }

type residentGalleryRPCRequest struct {
	Operation      string `json:"operation"`
	ResidentRef    string `json:"resident_ref,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type residentGalleryRPCResponse struct {
	Status string `json:"status"`
	Result struct {
		Resident  ResidentGalleryView `json:"resident"`
		Duplicate bool                `json:"duplicate"`
	} `json:"result"`
}

func (s busResidentGalleryService) call(ctx context.Context, request residentGalleryRPCRequest) (ResidentGalleryView, bool, error) {
	if s.client == nil {
		return ResidentGalleryView{}, false, errors.New("resident service unavailable")
	}
	if err := ctx.Err(); err != nil {
		return ResidentGalleryView{}, false, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return ResidentGalleryView{}, false, err
	}
	response, err := s.client.RequestWithTimeout(residentGalleryRPC, "discovery", body, "core", 5*time.Second)
	if err != nil || response == nil {
		return ResidentGalleryView{}, false, errors.New("resident service unavailable")
	}
	var decoded residentGalleryRPCResponse
	if json.Unmarshal(response.Payload, &decoded) != nil || decoded.Status != "ok" || decoded.Result.Resident.ResidentRef == "" {
		return ResidentGalleryView{}, false, errors.New("resident operation unavailable")
	}
	return decoded.Result.Resident, decoded.Result.Duplicate, nil
}

func (s busResidentGalleryService) Create(ctx context.Context, ref, key string) (ResidentGalleryView, bool, error) {
	return s.call(ctx, residentGalleryRPCRequest{Operation: "create", ResidentRef: strings.TrimSpace(ref), IdempotencyKey: strings.TrimSpace(key)})
}

func (s busResidentGalleryService) Status(ctx context.Context, ref string) (ResidentGalleryView, error) {
	view, _, err := s.call(ctx, residentGalleryRPCRequest{Operation: "status", ResidentRef: strings.TrimSpace(ref)})
	return view, err
}

func (s busResidentGalleryService) Rollback(ctx context.Context, ref string) (ResidentGalleryView, error) {
	view, _, err := s.call(ctx, residentGalleryRPCRequest{Operation: "rollback", ResidentRef: strings.TrimSpace(ref)})
	return view, err
}

func (s busResidentGalleryService) Delete(ctx context.Context, ref string) (ResidentGalleryView, error) {
	view, _, err := s.call(ctx, residentGalleryRPCRequest{Operation: "delete", ResidentRef: strings.TrimSpace(ref)})
	return view, err
}
