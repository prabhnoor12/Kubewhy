package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

// Entry is one persisted diagnosis result.
type Entry struct {
	Timestamp time.Time    `json:"timestamp"`
	Report    model.Report `json:"report"`
	Pod       PodRef       `json:"pod"`
	Status    string       `json:"status"`
}

// PodRef identifies the pod that was diagnosed.
type PodRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Filter controls which entries are returned by Query.
type Filter struct {
	Pod       string
	Namespace string
	Status    string
	Since     time.Time
	Last      int
}

// Store appends to and reads a JSONL history file.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore creates a Store. If dir is empty, uses ~/.kubewhy/.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("history: cannot determine home directory: %w", err)
		}
		dir = filepath.Join(home, ".kubewhy")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("history: cannot create directory %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "history.jsonl")}, nil
}

// Append writes one entry to the history file.
func (s *Store) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("history: cannot open file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("history: cannot marshal entry: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("history: cannot write entry: %w", err)
	}
	return nil
}

// Query reads the history file and returns entries matching the filter.
func (s *Store) Query(filter Filter) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("history: cannot open file: %w", err)
	}
	defer f.Close()

	var all []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if matchesFilter(entry, filter) {
			all = append(all, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("history: error reading file: %w", err)
	}

	if filter.Last > 0 && len(all) > filter.Last {
		all = all[len(all)-filter.Last:]
	}
	return all, nil
}

// Path returns the history file path.
func (s *Store) Path() string {
	return s.path
}

func matchesFilter(entry Entry, filter Filter) bool {
	if filter.Pod != "" && entry.Pod.Name != filter.Pod {
		return false
	}
	if filter.Namespace != "" && entry.Pod.Namespace != filter.Namespace {
		return false
	}
	if filter.Status != "" && entry.Status != filter.Status {
		return false
	}
	if !filter.Since.IsZero() && entry.Timestamp.Before(filter.Since) {
		return false
	}
	return true
}

func (s *Store) QueryByPattern(pod, namespace, rootCauseCode string, since time.Time) ([]Entry, error) {
	entries, err := s.Query(Filter{Pod: pod, Namespace: namespace, Since: since})
	if err != nil {
		return nil, err
	}
	var matched []Entry
	for _, entry := range entries {
		if entry.Report.RootCause != nil && entry.Report.RootCause.Code == rootCauseCode {
			matched = append(matched, entry)
		}
	}
	return matched, nil
}
