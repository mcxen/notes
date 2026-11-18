package settings

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSkinCleanKeepsOnlyWhatThePageCanDraw(t *testing.T) {
	bad := SkinLayer{Kind: "image", Image: "../../etc/passwd"}
	if got := bad.Clean(); got.Active() {
		t.Errorf("a path was taken as a picture: %+v", got)
	}
	if got := (SkinLayer{Kind: "gradient", From: "red;}", To: "#000000"}).Clean(); got.Active() {
		t.Errorf("a style was taken as a color: %+v", got)
	}
	l := SkinLayer{Kind: "gradient", From: "#112233", Via: "url(x)", To: "#445566", Angle: -90, Blur: 999, Veil: 400, Brightness: -999, OffsetX: 500}.Clean()
	if l.Via != "" || l.Angle != 270 || l.Blur != MaxBlur || l.Veil != 100 || l.Brightness != -60 || l.OffsetX != 100 {
		t.Errorf("numbers were not held to their limits: %+v", l)
	}
}

func TestSkinFileIsInsideTheSkinsFolder(t *testing.T) {
	settings := filepath.Join("data", "settings.json")
	if got := SkinFile(settings, "page-1a2b.png"); got != filepath.Join("data", "skins", "page-1a2b.png") {
		t.Errorf("SkinFile = %q", got)
	}
	for _, name := range []string{"", "../settings.json", "a/b.png", `a\b.png`, "x.exe", ".hidden.png"} {
		if got := SkinFile(settings, name); got != "" {
			t.Errorf("SkinFile(%q) = %q, want none", name, got)
		}
	}
}

func TestStartGivesAReadableLayer(t *testing.T) {
	l := SkinLayer{}.Start("gradient")
	if l.Veil != DefaultVeil || l.From == "" || l.To == "" || !l.Active() {
		t.Errorf("a new gradient: %+v", l)
	}
	l.Veil = 10
	if got := l.Start("gradient"); got.Veil != 10 {
		t.Errorf("starting again reset what the user set: %+v", got)
	}
}

func TestHistoryKeepsEachPictureOnceMostRecentFirst(t *testing.T) {
	var s Skin
	s.Remember("img-a.png")
	s.Remember("img-b.png")
	s.Remember("img-a.png")
	s.Remember("../x.png")
	if len(s.History) != 2 || s.History[0] != "img-a.png" || s.History[1] != "img-b.png" {
		t.Fatalf("History = %v", s.History)
	}
	for i := 0; i < 50; i++ {
		s.Remember(fmt.Sprintf("img-%d.png", i))
	}
	if len(s.History) != MaxHistory {
		t.Fatalf("the history holds %d pictures, want %d", len(s.History), MaxHistory)
	}
	s.Forget("img-49.png")
	if s.History[0] == "img-49.png" {
		t.Fatal("a forgotten picture is still in the history")
	}
}
