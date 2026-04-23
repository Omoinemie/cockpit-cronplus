package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	ConfPath  string
	LogDir    string
	SettingsPath string
	StatePath string

	taskMu sync.RWMutex
	logMu  sync.Mutex
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
	os.MkdirAll(logDir, 0755)
	return s
}

// atomicWrite writes content to path atomically via temp+rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp_*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
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
