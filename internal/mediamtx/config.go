package mediamtx

import (
	"errors"

	"gopkg.in/yaml.v3"
)

// RenderConfig replaces the wildcard MediaMTX path set with an explicit
// allowlist. The input remains the source of all non-path settings, including
// authentication configuration, and is never logged by this package.
func RenderConfig(base []byte, cameraIDs []string) ([]byte, error) {
	if len(base) == 0 {
		return nil, errors.New("MediaMTX base configuration is empty")
	}
	paths, err := DesiredPaths(cameraIDs)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := yaml.Unmarshal(base, &config); err != nil {
		return nil, errors.New("MediaMTX base configuration is invalid")
	}
	if config == nil {
		return nil, errors.New("MediaMTX base configuration is empty")
	}
	allowlist := make(map[string]any, len(paths))
	for _, path := range paths {
		allowlist[path] = map[string]any{"source": "publisher", "overridePublisher": true}
	}
	config["paths"] = allowlist
	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, errors.New("MediaMTX configuration could not be rendered")
	}
	return data, nil
}
