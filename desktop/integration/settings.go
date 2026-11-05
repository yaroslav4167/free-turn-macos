package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/samosvalishe/free-turn-proxy/desktop/locale"
)

type Preferences struct {
	Language            string `json:"language"`
	CloseToTray         bool   `json:"closeToTray"`
	AutoConnect         bool   `json:"autoConnect"`
	AutoConnectServerID string `json:"autoConnectServerId"`
	AutoCheckUpdates    bool   `json:"autoCheckUpdates"`
}

func preferencesPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "FreeTurnDesktop", "desktop.json"), err
}

func LoadPreferences() (Preferences, error) {
	prefs := Preferences{Language: "system", CloseToTray: true, AutoCheckUpdates: true}
	path, err := preferencesPath()
	if err != nil {
		return prefs, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}
	err = json.Unmarshal(data, &prefs)
	if !ValidLanguage(prefs.Language) {
		prefs.Language = "system"
	}
	return prefs, err
}

func ValidLanguage(value string) bool { return value == "system" || locale.Supported(value) }

func SystemLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if value == "" {
			continue
		}
		return locale.MatchSystemLanguage(value)
	}
	return locale.MatchSystemLanguage(nativeSystemLanguage())
}

func SavePreferences(prefs Preferences) error {
	path, err := preferencesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".desktop-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
