package runbook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMapping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runbooks.json")
	data := `{"codes":{"oom_killed":"https://runbooks.example.com/oom","crash_loop":"https://runbooks.example.com/crash"}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Lookup("oom_killed"); got != "https://runbooks.example.com/oom" {
		t.Errorf("oom_killed = %q", got)
	}
	if got := m.Lookup("unknown_code"); got != "" {
		t.Errorf("unknown_code = %q, want empty", got)
	}
}

func TestLookupNilMapping(t *testing.T) {
	var m *Mapping
	if got := m.Lookup("oom_killed"); got != "" {
		t.Errorf("nil mapping lookup = %q, want empty", got)
	}
}
