package diagnosis

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// parseQuantity supports the CPU and memory formats most commonly found in
// Pod resource requests. CPU is returned in millicores; memory in bytes.
func parseQuantity(raw string, memory bool) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	quantity, err := resource.ParseQuantity(raw)
	if err != nil || quantity.Sign() < 0 {
		return 0, false
	}
	if memory {
		return quantity.Value(), true
	}
	return quantity.MilliValue(), true
}
