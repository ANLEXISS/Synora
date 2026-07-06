package topology

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type yamlResidents struct {
	Residents []Resident `yaml:"residents"`
}

func LoadResidents(path string) (map[string]*Resident, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var y yamlResidents

	if err := yaml.Unmarshal(data, &y); err != nil {
		return nil, err
	}

	if len(y.Residents) == 0 {
		return nil, fmt.Errorf("no residents defined")
	}

	out := make(map[string]*Resident)

	for i := range y.Residents {

		r := &y.Residents[i]

		if r.ID == "" {
			return nil, fmt.Errorf("resident with empty ID")
		}

		if _, exists := out[r.ID]; exists {
			return nil, fmt.Errorf("duplicate resident ID: %s", r.ID)
		}

		out[r.ID] = r
	}

	return out, nil
}


func SaveResidents(path string, residents map[string]*Resident) error {

	var y yamlResidents

	for _, r := range residents {
		y.Residents = append(y.Residents, *r)
	}

	data, err := yaml.Marshal(y)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}
