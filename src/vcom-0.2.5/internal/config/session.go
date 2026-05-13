package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// SessionState stores the UI state that should be restored on next launch.
type SessionState struct {
	ActivePane string         `toml:"active_pane"`
	Left       PaneSession    `toml:"left"`
	Right      PaneSession    `toml:"right"`
}

// PaneSession stores per-pane session state (path and selected entry name).
type PaneSession struct {
	Path         string            `toml:"path"`
	EntryName    string            `toml:"entry_name"`
	CursorMemory map[string]string `toml:"cursor_memory"`
}

// DefaultSessionPath returns the path to the session file.
func DefaultSessionPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	xdgDir := os.Getenv("XDG_CONFIG_HOME")
	if xdgDir == "" {
		xdgDir = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(xdgDir, "vcom", "session.toml"), nil
}

// LoadSession reads the session state from disk.
// Returns (SessionState, nil) on success, (empty, error) if file doesn't exist.
func LoadSession() (SessionState, error) {
	path, err := DefaultSessionPath()
	if err != nil {
		return SessionState{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SessionState{}, nil
		}
		return SessionState{}, fmt.Errorf("read %s: %w", path, err)
	}

	var s SessionState
	if err := toml.Unmarshal(data, &s); err != nil {
		return SessionState{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// SaveSession writes the session state to disk.
func SaveSession(s SessionState) error {
	path, err := DefaultSessionPath()
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(absPath), err)
	}

	data, err := toml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
