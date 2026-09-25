package history

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

func testReport(name, namespace, status string) model.Report {
	return model.Report{
		GeneratedAt: time.Now().UTC(),
		Pod:         model.PodIdentity{Name: name, Namespace: namespace},
		Status:      status,
		Confidence:  "high",
		Summary:     "test summary",
	}
}

func testEntry(name, namespace, status string) Entry {
	return Entry{
		Timestamp: time.Now().UTC(),
		Report:    testReport(name, namespace, status),
		Pod:       PodRef{Name: name, Namespace: namespace},
		Status:    status,
	}
}

func TestNewStoreCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore(%q) error: %v", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}
	if store.Path() != filepath.Join(dir, "history.jsonl") {
		t.Errorf("Path() = %q, want %q", store.Path(), filepath.Join(dir, "history.jsonl"))
	}
}

func TestAppendCreatesFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(testEntry("api", "default", "healthy")); err != nil {
		t.Fatalf("Append error: %v", err)
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestAppendWritesOneLinePerEntry(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := store.Append(testEntry("api", "default", "healthy")); err != nil {
			t.Fatalf("Append %d error: %v", i, err)
		}
	}
	entries, err := store.Query(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("got %d entries, want 5", len(entries))
	}
}

func TestQueryFiltersByPod(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Append(testEntry("api", "default", "healthy"))
	_ = store.Append(testEntry("web", "default", "broken"))
	_ = store.Append(testEntry("api", "default", "degraded"))

	entries, err := store.Query(Filter{Pod: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestQueryFiltersByNamespace(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Append(testEntry("api", "payments", "healthy"))
	_ = store.Append(testEntry("api", "default", "broken"))

	entries, err := store.Query(Filter{Namespace: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

func TestQueryFiltersByStatus(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Append(testEntry("api", "default", "healthy"))
	_ = store.Append(testEntry("web", "default", "broken"))
	_ = store.Append(testEntry("db", "default", "broken"))

	entries, err := store.Query(Filter{Status: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestQueryFiltersBySince(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := testEntry("api", "default", "healthy")
	old.Timestamp = time.Now().Add(-48 * time.Hour)
	_ = store.Append(old)

	recent := testEntry("web", "default", "broken")
	recent.Timestamp = time.Now().Add(-1 * time.Hour)
	_ = store.Append(recent)

	entries, err := store.Query(Filter{Since: time.Now().Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
	if entries[0].Pod.Name != "web" {
		t.Errorf("got pod %q, want web", entries[0].Pod.Name)
	}
}

func TestQueryLastN(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_ = store.Append(testEntry("api", "default", "healthy"))
	}
	entries, err := store.Query(Filter{Last: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}
}

func TestQueryEmptyFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	entries, err := store.Query(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestQueryMissingFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.Query(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Errorf("got %v, want nil", entries)
	}
}

func TestAppendConcurrent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = store.Append(testEntry("api", "default", "healthy"))
		}(i)
	}
	wg.Wait()

	entries, err := store.Query(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Errorf("got %d entries, want 20", len(entries))
	}
}
