package backend

import (
	"testing"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend/routing"
)

func TestRoutingListsPersistAndDoNotAliasStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	srv := DefaultServer()
	srv.RoutingLists = []routing.List{{Name: "Bypass", Mode: routing.Blacklist, Enabled: true, Entries: []string{"EXAMPLE.COM"}}}
	saved, err := store.Save(srv)
	if err != nil {
		t.Fatal(err)
	}
	saved.RoutingLists[0].Entries[0] = "changed.example"
	list := store.List()
	list[0].RoutingLists[0].Mode = routing.Whitelist
	loaded, ok := store.Get(saved.ID)
	if !ok || loaded.RoutingLists[0].Entries[0] != "example.com" || loaded.RoutingLists[0].Mode != routing.Blacklist {
		t.Fatal("store aliased returned values")
	}
	loaded.RoutingLists[0].Entries[0] = "other.example"
	fresh, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok = fresh.Get(saved.ID)
	if !ok || loaded.RoutingLists[0].Entries[0] != "example.com" {
		t.Fatal("rules not persisted")
	}
	if DefaultServer().RoutingLists == nil {
		t.Fatal("new profiles must expose an empty list array")
	}
}
