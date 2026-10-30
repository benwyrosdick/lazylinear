package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMovesLegacyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	legacyDir := filepath.Join(home, ".lazylinear")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(`{"api_key":"key"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "key")
	}
	info, err := os.Stat(filepath.Join(home, ".config", "lazylinear", "config.json"))
	if err != nil {
		t.Fatalf("config was not moved: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("moved config has mode %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Errorf("legacy directory still exists: %v", err)
	}
}

func TestLoadKeepsExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := (&Config{APIKey: "new"}).Save(); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(home, ".lazylinear")
	os.MkdirAll(legacyDir, 0o755)
	os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(`{"api_key":"old"}`), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "new" {
		t.Errorf("APIKey = %q, want the XDG config's %q", cfg.APIKey, "new")
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "config.json")); err != nil {
		t.Errorf("legacy config should be left alone when an XDG config exists: %v", err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	empty, err := LoadState()
	if err != nil || *empty != (State{}) {
		t.Fatalf("LoadState with no file = %+v, %v; want empty state", empty, err)
	}

	want := State{TeamID: "team", Status: "All", CustomViewID: "view", ProjectID: "project", MyIssues: true}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if *got != want {
		t.Errorf("LoadState = %+v, want %+v", *got, want)
	}
}
