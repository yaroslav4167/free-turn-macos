package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoConnectDefaultsAndMigration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prefs, err := LoadPreferences()
	if err != nil || prefs.AutoConnect || !prefs.AutoCheckUpdates {
		t.Fatalf("autoconnect must default off: %+v, %v", prefs, err)
	}
	path, _ := preferencesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"closeToTray":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs, err = LoadPreferences()
	if err != nil || prefs.AutoConnect || prefs.CloseToTray || !prefs.AutoCheckUpdates {
		t.Fatalf("bad migration: %+v, %v", prefs, err)
	}
}

func TestObsoleteRenderingPreferencesAreIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, _ := preferencesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, oldJSON := range []string{
		`{}`, `{"renderingMode":"auto"}`, `{"renderingMode":"hardware"}`, `{"renderingMode":"software"}`,
	} {
		if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
			t.Fatal(err)
		}
		prefs, err := LoadPreferences()
		if err != nil || !prefs.CloseToTray || !prefs.AutoCheckUpdates {
			t.Fatalf("migration of %s: %+v, %v", oldJSON, prefs, err)
		}
		if err := SavePreferences(prefs); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), "renderingMode") {
			t.Fatalf("obsolete preference was persisted: %s, %v", data, err)
		}
	}
}
