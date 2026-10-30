package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the application configuration
type Config struct {
	APIKey string `json:"api_key"`
}

// State is the view the app was last left on, restored at startup
type State struct {
	TeamID       string `json:"team_id,omitempty"`
	Status       string `json:"status,omitempty"`
	CustomViewID string `json:"custom_view_id,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	MyIssues     bool   `json:"my_issues,omitempty"`
}

// ConfigPath returns the config file location: $XDG_CONFIG_HOME/lazylinear/config.json,
// falling back to ~/.config/lazylinear/config.json
func ConfigPath() (string, error) {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// StatePath returns the state file location: $XDG_STATE_HOME/lazylinear/state.json,
// falling back to ~/.local/state/lazylinear/state.json
func StatePath() (string, error) {
	dir, err := xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

// xdgDir returns the lazylinear directory under the base directory named by env,
// or under fallback in the home directory when env is unset or not absolute
func xdgDir(env string, fallback string) (string, error) {
	if base := os.Getenv(env); filepath.IsAbs(base) {
		return filepath.Join(base, "lazylinear"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback, "lazylinear"), nil
}

// Load loads configuration from file, moving a config from the old ~/.lazylinear location first
func Load() (*Config, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	if err := migrateLegacyConfig(configPath); err != nil {
		return nil, fmt.Errorf("moving config to %s: %w", configPath, err)
	}

	var config Config
	if err := readJSON(configPath, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// Save saves configuration to file, readable only by the user since it holds the API key
func (c *Config) Save() error {
	configPath, err := ConfigPath()
	if err != nil {
		return err
	}
	return writeJSON(configPath, c)
}

// LoadState loads the saved view state, returning an empty state if none was saved
func LoadState() (*State, error) {
	statePath, err := StatePath()
	if err != nil {
		return nil, err
	}
	var state State
	if err := readJSON(statePath, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Save saves the view state to file
func (s *State) Save() error {
	statePath, err := StatePath()
	if err != nil {
		return err
	}
	return writeJSON(statePath, s)
}

// migrateLegacyConfig moves ~/.lazylinear/config.json to configPath when only the old file exists
func migrateLegacyConfig(configPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	legacyDir := filepath.Join(home, ".lazylinear")
	legacyPath := filepath.Join(legacyDir, "config.json")
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	data, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Remove(legacyPath); err != nil {
		return err
	}
	// Only removes the old directory if nothing else is in it
	os.Remove(legacyDir)
	return nil
}

// readJSON decodes the JSON file at path into v, leaving v unchanged if the file doesn't exist
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON writes v to path as JSON, creating its directory, with permissions for the user only
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
