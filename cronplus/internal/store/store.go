package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultConfDir  = "/opt/cronplus"
	DefaultConfFile = "/opt/cronplus/tasks.conf"
	DefaultLogDir   = "/opt/cronplus/logs"
	DefaultSettings = "/opt/cronplus/settings.json"
	DefaultState    = "/opt/cronplus/state.json"
)

// Store manages all file-based persistence.
type Store struct {
	ConfPath     string
	LogDir       string
	SettingsPath string
	StatePath    string

	taskMu sync.RWMutex
	logMu  sync.Mutex

	// Tasks whose log file has already been checked for the legacy JSON-array
	// format (guarded by logMu).
	migrated map[int]bool

	// In-memory cache of state.json, flushed periodically by the daemon.
	stateMu    sync.Mutex
	state      *DaemonState
	stateDirty bool
}

func New(confPath, logDir string) *Store {
	if confPath == "" {
		confPath = DefaultConfFile
	}
	if logDir == "" {
		logDir = DefaultLogDir
	}
	s := &Store{
		ConfPath:     confPath,
		LogDir:       logDir,
		SettingsPath: filepath.Join(filepath.Dir(confPath), "settings.json"),
		StatePath:    filepath.Join(filepath.Dir(confPath), "state.json"),
	}
	os.MkdirAll(logDir, 0700)
	return s
}

// atomicWrite writes content to path atomically via temp+rename.
// Uses restrictive permissions (0600) to prevent other users from reading config.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp_*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Set restrictive permissions before writing sensitive data
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	// Sync to ensure data is flushed before rename
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ReadJSON reads a JSON file into v. Returns false if file doesn't exist.
func ReadJSON(path string, v interface{}) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(data) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(data, v)
}

// writeJSON marshals v and writes atomically.
func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}

// CleanupTempFiles removes leftover atomicWrite temp files. A crash (or a full
// disk) can leave .tmp_* files behind in the config and log directories.
func (s *Store) CleanupTempFiles() (int, error) {
	dirs := []string{filepath.Dir(s.ConfPath), s.LogDir}
	removed := 0
	var firstErr error
	cutoff := time.Now().Add(-time.Hour)

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), ".tmp_") {
				continue
			}
			info, err := e.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
				removed++
			}
		}
	}
	return removed, firstErr
}
