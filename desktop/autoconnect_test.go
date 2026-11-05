package main

import (
	"errors"
	"testing"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend"
	"github.com/samosvalishe/free-turn-proxy/desktop/integration"
)

func TestConnectOnLaunch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a := &App{preferences: integration.Preferences{AutoConnect: enabled, AutoConnectServerID: "chosen"}}
		calls := 0
		failure := errors.New("offline")
		connect := func(id string) error {
			calls++
			if id != "chosen" {
				t.Fatalf("wrong profile: %s", id)
			}
			return failure
		}
		err := a.connectOnLaunch(connect)
		if enabled && !errors.Is(err, failure) {
			t.Fatalf("lost connection error: %v", err)
		}
		if !enabled && err != nil {
			t.Fatal(err)
		}
		if err := a.connectOnLaunch(connect); err != nil {
			t.Fatal(err)
		}
		want := 0
		if enabled {
			want = 1
		}
		if calls != want {
			t.Fatalf("calls = %d, want %d", calls, want)
		}
	}
}

func TestAutoConnectPreferences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := backend.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := store.Save(backend.DefaultServer())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, preferences: integration.Preferences{CloseToTray: true}}
	if err := a.SetAutoConnect(true, "missing"); err == nil {
		t.Fatal("accepted missing profile")
	}
	if err := a.SetAutoConnect(true, srv.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCloseToTray(false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetAutoCheckUpdates(true); err != nil {
		t.Fatal(err)
	}
	prefs, err := integration.LoadPreferences()
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.AutoConnect || prefs.AutoConnectServerID != srv.ID || prefs.CloseToTray || !prefs.AutoCheckUpdates {
		t.Fatalf("settings lost: %+v", prefs)
	}
	if err := store.Delete(srv.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SetAutoConnect(false, srv.ID); err != nil {
		t.Fatalf("cannot disable deleted profile: %v", err)
	}
}
