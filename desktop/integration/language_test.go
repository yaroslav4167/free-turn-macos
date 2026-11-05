package integration

import (
	"github.com/samosvalishe/free-turn-proxy/desktop/locale"
	"os"
	"testing"
)

func TestSystemLanguagePrecedenceAndFallback(t *testing.T) {
	for _, test := range []struct{ all, messages, lang, want string }{
		{"", "", "ru_RU.UTF-8", "ru"}, {"", "", "en_US.UTF-8", "en"},
		{"", "", "de_DE.UTF-8", "en"}, {"C", "ru_RU.UTF-8", "ru_RU.UTF-8", "en"},
		{"ru_RU.UTF-8", "en_US.UTF-8", "en_US.UTF-8", "ru"},
		{"", "ru_RU.UTF-8", "en_US.UTF-8", "ru"}, {"", "", "", "en"},
		{"", "", "RU-ru", "ru"}, {"", "", "russian", "en"},
	} {
		t.Setenv("LC_ALL", test.all)
		t.Setenv("LC_MESSAGES", test.messages)
		t.Setenv("LANG", test.lang)
		want := test.want
		if test.all == "" && test.messages == "" && test.lang == "" {
			want = locale.MatchSystemLanguage(nativeSystemLanguage())
		}
		if got := SystemLanguage(); got != want {
			t.Fatalf("%+v: got %q", test, got)
		}
	}
}

func TestLanguagePreferenceMigrationAndPersistence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prefs, err := LoadPreferences()
	if err != nil || prefs.Language != "system" {
		t.Fatalf("defaults: %+v, %v", prefs, err)
	}
	for _, language := range []string{"ru", "en", "system"} {
		prefs.Language = language
		if err := SavePreferences(prefs); err != nil {
			t.Fatal(err)
		}
		got, err := LoadPreferences()
		if err != nil || got.Language != language {
			t.Fatalf("persistence: %+v, %v", got, err)
		}
	}
	path, _ := preferencesPath()
	for _, data := range []string{`{"closeToTray":true}`, `{"language":"unsupported"}`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadPreferences()
		if err != nil || got.Language != "system" {
			t.Fatalf("migration: %+v, %v", got, err)
		}
	}
	if ValidLanguage("de") || !ValidLanguage("system") {
		t.Fatal("invalid supported languages")
	}
}
