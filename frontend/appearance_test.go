package frontend

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Drawing the appearance page, without touching it, must not change the
// settings: only a change the user makes is saved.
func TestAppearancePageDoesNotWriteUnchangedSettings(t *testing.T) {
	a := newTestApp(t)
	before, err := os.ReadFile(a.settings)
	if err != nil {
		t.Fatal(err)
	}
	s := &settingsWindow{a: a, page: "appearance"}
	s.refresh()
	tester := ui.NewTester(s.view, 760, 540)
	for i := 0; i < 3; i++ {
		tester.Frame()
	}
	after, err := os.ReadFile(a.settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("drawing the page changed the settings:\n%s\n---\n%s", before, after)
	}
}
