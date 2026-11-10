package settings

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestRememberKeepsMostRecentFirstAndBounded(t *testing.T) {
	var s Settings
	for i := 0; i < MaxRecent+3; i++ {
		s.Remember(fmt.Sprintf("/vault/%02d", i))
	}
	if len(s.Recent) != MaxRecent {
		t.Fatalf("len = %d, want %d", len(s.Recent), MaxRecent)
	}
	if s.Recent[0] != fmt.Sprintf("/vault/%02d", MaxRecent+2) {
		t.Fatalf("most recent first, got %q", s.Recent[0])
	}
	existing := s.Recent[5]
	s.Remember(existing)
	if s.Recent[0] != existing || len(s.Recent) != MaxRecent {
		t.Fatalf("remembering %q should move it to the front: %v", existing, s.Recent)
	}
	count := 0
	for _, f := range s.Recent {
		if f == existing {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%q appears %d times", existing, count)
	}
}

func TestRemotesAreKeyedByWorkingCopy(t *testing.T) {
	var s Settings
	s.SetRemote("/copy/a", Remote{Kind: "webdav", Address: "https://dav.example/Notes/"})
	if r, ok := s.RemoteOf("/copy/a"); !ok || r.Kind != "webdav" {
		t.Fatalf("remote = %+v %v", r, ok)
	}
	if _, ok := s.RemoteOf("/copy/b"); ok {
		t.Fatal("an unrelated folder has no remote")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	empty, err := Load(path)
	if err != nil || len(empty.Recent) != 0 {
		t.Fatalf("missing file should load as empty settings: %v %+v", err, empty)
	}
	s := Settings{}
	s.Remember("/a")
	s.Remember("/b")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Recent) != 2 || got.Recent[0] != "/b" {
		t.Fatalf("round trip = %+v", got)
	}
	got.Forget("/b")
	if len(got.Recent) != 1 || got.Recent[0] != "/a" {
		t.Fatalf("forget = %+v", got)
	}
}

func TestAppearanceDefaultsToTranslucentSidebar(t *testing.T) {
	var s Settings
	if s.OpaqueSidebar {
		t.Fatal("the sidebar is translucent unless the user turns it off")
	}
	if s.PickerAtLaunch {
		t.Fatal("the last vault reopens at launch unless the user asks for the picker")
	}
}
