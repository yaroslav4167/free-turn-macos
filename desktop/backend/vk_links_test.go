package backend

import (
	"reflect"
	"testing"
)

func TestNormalizeVKLinksAtSaveBoundary(t *testing.T) {
	original := []string{" https://vk.ru/call/join/one ", "", " \t ", "https://vk.ru/call/join/two\r", ""}
	srv := Server{VKLinks: original}
	normalizeServer(&srv)
	want := []string{"https://vk.ru/call/join/one", "https://vk.ru/call/join/two"}
	if !reflect.DeepEqual(srv.VKLinks, want) {
		t.Fatalf("unexpected saved links: %#v", srv.VKLinks)
	}
	if original[0] != " https://vk.ru/call/join/one " || len(original) != 5 {
		t.Fatal("normalization mutated the input slice")
	}
	srv.VKLinks = nil
	normalizeServer(&srv)
	if srv.VKLinks == nil || len(srv.VKLinks) != 0 {
		t.Fatal("empty links must remain a JSON array")
	}
}

func TestSavedVKLinksExcludeEditingBlankLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(Server{VKLinks: []string{" first ", "", "second\r", ""}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second"}
	if !reflect.DeepEqual(saved.VKLinks, want) {
		t.Fatalf("saved: %#v", saved.VKLinks)
	}
	reloaded, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := reloaded.Get(saved.ID)
	if !ok || !reflect.DeepEqual(profile.VKLinks, want) {
		t.Fatalf("reloaded: %#v", profile.VKLinks)
	}
}
