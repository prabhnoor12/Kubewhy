package runbook

import (
	"encoding/json"
	"fmt"
	"os"
)

type Mapping struct {
	Codes map[string]string `json:"codes"`
}

func LoadMapping(path string) (*Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("runbook: read %s: %w", path, err)
	}
	var m Mapping
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("runbook: parse %s: %w", path, err)
	}
	return &m, nil
}

func (m *Mapping) Lookup(code string) string {
	if m == nil || m.Codes == nil {
		return ""
	}
	return m.Codes[code]
}
