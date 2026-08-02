package durableworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func ValidateLayerGraph() error {
	seen := map[LayerKind]struct{}{}
	for _, layer := range layerOrder {
		if !validLayer(layer) {
			return ErrInvalidLayer
		}
		if _, ok := seen[layer]; ok {
			return ErrInvalidLayer
		}
		seen[layer] = struct{}{}
	}
	if len(seen) != 7 {
		return ErrInvalidLayer
	}
	return nil
}

func SchemaFingerprint() string {
	payload, _ := json.Marshal(struct {
		Version string
		Layers  []LayerKind
	}{"durable-workflow-schema-v1", layerOrder})
	digest := sha256.Sum256(payload)
	return "durable-workflow-schema-v1:" + hex.EncodeToString(digest[:])
}
