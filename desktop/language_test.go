package main

import (
	"testing"

	"github.com/samosvalishe/free-turn-proxy/desktop/integration"
	"github.com/samosvalishe/free-turn-proxy/desktop/locale"
)

func TestSetLanguagePersistsAndRejectsUnsupportedValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	t.Cleanup(func() { locale.Set("en") })
	app := &App{preferences: integration.Preferences{Language: "system", AutoCheckUpdates: true}}
	for _, test := range []struct{ preference, text string }{{"en", "Settings"}, {"ru", "Настройки"}, {"system", "Настройки"}} {
		if err := app.SetLanguage(test.preference); err != nil {
			t.Fatal(err)
		}
		prefs, err := integration.LoadPreferences()
		if err != nil || prefs.Language != test.preference || !prefs.AutoCheckUpdates {
			t.Fatalf("saved prefs: %+v, %v", prefs, err)
		}
		if text := locale.Text("Настройки", "Settings"); text != test.text {
			t.Fatalf("native translation: %q", text)
		}
	}
	if err := app.SetLanguage("de"); err == nil {
		t.Fatal("unsupported language accepted")
	}
	if app.preferences.Language != "system" {
		t.Fatal("invalid language changed preference")
	}
}
