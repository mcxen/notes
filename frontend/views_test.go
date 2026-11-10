package frontend

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
)

// newTestApp is the app's actions over a temporary settings file that
// remembers two vault folders.
func newTestApp(t *testing.T) *appActions {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	st := settings.Settings{Recent: []string{filepath.Join(dir, "Work"), filepath.Join(dir, "Home")}}
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	return &appActions{notebooks: newNotebooks(), settings: path}
}

// shot writes a view's picture next to the test's output when NOTES_SHOTS names
// a folder, so the layout can be looked at.
func shot(t *testing.T, tester *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("NOTES_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tester.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestPickerListsRecentVaultsAndOpensThem(t *testing.T) {
	a := newTestApp(t)
	p := &picker{a: a, recent: a.recentFolders()}
	tester := ui.NewTester(p.view, 520, 440)
	tester.Frame()
	if !tester.HasText("Choose a vault") || !tester.HasText("Work") || !tester.HasText("Home") {
		t.Fatalf("picker texts = %v", tester.Texts())
	}
	shot(t, tester, "picker")
}

func TestSettingsPagesDraw(t *testing.T) {
	a := newTestApp(t)
	s := &settingsWindow{a: a, page: "general"}
	s.refresh()
	tester := ui.NewTester(s.view, 760, 540)
	tester.Frame()
	if !tester.HasText("General") || !tester.HasText("Show the vault picker when Notes opens") {
		t.Fatalf("general texts = %v", tester.Texts())
	}
	shot(t, tester, "settings-general")
	for _, page := range []struct{ id, marker string }{
		{"appearance", "Appearance"},
		{"vaults", "Remove"},
		{"sync", "Connect to a remote folder"},
		{"about", "Settings file"},
	} {
		s.page = page.id
		tester.Frame()
		if !tester.HasText(page.marker) {
			t.Fatalf("page %s lacks %q: %v", page.id, page.marker, tester.Texts())
		}
		shot(t, tester, "settings-"+page.id)
	}
}

func TestSettingsToggleSavesThePickerChoice(t *testing.T) {
	a := newTestApp(t)
	s := &settingsWindow{a: a, page: "general"}
	s.refresh()
	tester := ui.NewTester(s.view, 760, 540)
	tester.Frame()
	// The switch sits at the right end of its row, level with the label: the
	// card's content ends 8 + 24 + 16 from the window's right edge.
	label, ok := tester.Find("Show the vault picker when Notes opens")
	if !ok {
		t.Fatalf("the switch's label is missing: %v", tester.Texts())
	}
	tester.ClickAt(760-8-24-16-19, label.Y+label.H/2+8)
	tester.Frame()
	st, err := settings.Load(a.settings)
	if err != nil {
		t.Fatal(err)
	}
	if !st.PickerAtLaunch {
		raw, _ := os.ReadFile(a.settings)
		t.Fatalf("the toggle should be saved; settings file = %s", raw)
	}
	var check map[string]any
	raw, _ := os.ReadFile(a.settings)
	if err := json.Unmarshal(raw, &check); err != nil || check["pickerAtLaunch"] != true {
		t.Fatalf("settings file = %s", raw)
	}
}
