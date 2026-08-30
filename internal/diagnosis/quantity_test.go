package diagnosis

import "testing"

func TestParseQuantityUsesKubernetesSemantics(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		memory bool
		want   int64
	}{
		{name: "cpu millicores", raw: "500m", want: 500},
		{name: "cpu decimal", raw: "0.5", want: 500},
		{name: "cpu scientific notation", raw: "1e-3", want: 1},
		{name: "memory binary", raw: "1.5Gi", memory: true, want: 1610612736},
		{name: "memory decimal", raw: "100M", memory: true, want: 100000000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseQuantity(test.raw, test.memory)
			if !ok || got != test.want {
				t.Fatalf("parseQuantity(%q, %t) = (%d, %t), want (%d, true)", test.raw, test.memory, got, ok, test.want)
			}
		})
	}
}

func TestParseQuantityRejectsInvalidOrNegativeValues(t *testing.T) {
	for _, raw := range []string{"", "not-a-quantity", "-1Gi"} {
		if got, ok := parseQuantity(raw, true); ok || got != 0 {
			t.Errorf("parseQuantity(%q, true) = (%d, %t), want (0, false)", raw, got, ok)
		}
	}
}
