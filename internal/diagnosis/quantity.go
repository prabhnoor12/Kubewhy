package diagnosis

import (
	"strconv"
	"strings"
)

// parseQuantity supports the CPU and memory formats most commonly found in
// Pod resource requests. CPU is returned in millicores; memory in bytes.
func parseQuantity(raw string, memory bool) (int64, bool) {
	if raw == "" {
		return 0, false
	}
	raw = strings.TrimSpace(raw)
	if memory {
		return parseMemory(raw)
	}
	if strings.HasSuffix(raw, "m") {
		value, err := strconv.ParseInt(strings.TrimSuffix(raw, "m"), 10, 64)
		return value, err == nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return int64(value * 1000), true
}

func parseMemory(raw string) (int64, bool) {
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
		{"K", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12},
	}
	for _, unit := range units {
		if strings.HasSuffix(raw, unit.suffix) {
			value, err := strconv.ParseFloat(strings.TrimSuffix(raw, unit.suffix), 64)
			return int64(value * float64(unit.multiplier)), err == nil
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil
}
