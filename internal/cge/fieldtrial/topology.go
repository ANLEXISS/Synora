package fieldtrial

import (
	"encoding/json"
	"fmt"
	"os"

	cgecontext "synora/internal/cge/context"
)

func LoadTopologyFile(path string) (cgecontext.TopologySnapshot, error) {
	if path == "" {
		return cgecontext.TopologySnapshot{}, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return cgecontext.TopologySnapshot{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return cgecontext.TopologySnapshot{}, fmt.Errorf("%w: topology path", ErrInvalidConfig)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cgecontext.TopologySnapshot{}, err
	}
	var topology cgecontext.TopologySnapshot
	if err := json.Unmarshal(data, &topology); err != nil {
		return cgecontext.TopologySnapshot{}, err
	}
	if err := topology.Validate(); err != nil {
		return cgecontext.TopologySnapshot{}, err
	}
	return topology.Clone(), nil
}
