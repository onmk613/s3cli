package action

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// mirrorManifest is append-only: a key is recorded only after its copy
// operation succeeds. The next --resume run still performs a complete list,
// so deletes are calculated from current inventories rather than this file.
type mirrorManifest struct {
	completed map[string]struct{}
	file      *os.File
	mu        sync.Mutex
}

func openMirrorManifest(path string, resume bool) (*mirrorManifest, error) {
	if path == "" {
		if resume {
			return nil, fmt.Errorf("mirror --resume requires --manifest")
		}
		return nil, nil
	}
	m := &mirrorManifest{completed: make(map[string]struct{})}
	if resume {
		input, err := os.Open(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			scanner := bufio.NewScanner(input)
			for scanner.Scan() {
				if key := strings.TrimSpace(scanner.Text()); key != "" {
					m.completed[key] = struct{}{}
				}
			}
			closeErr := input.Close()
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	m.file = file
	return m, nil
}

func (m *mirrorManifest) has(key string) bool {
	if m == nil {
		return false
	}
	_, ok := m.completed[key]
	return ok
}

func (m *mirrorManifest) mark(key string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.completed[key]; exists {
		return nil
	}
	if _, err := m.file.WriteString(key + "\n"); err != nil {
		return err
	}
	if err := m.file.Sync(); err != nil {
		return err
	}
	m.completed[key] = struct{}{}
	return nil
}

func (m *mirrorManifest) close() error {
	if m == nil || m.file == nil {
		return nil
	}
	return m.file.Close()
}
