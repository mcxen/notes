package frontend

import (
	"os"
	"path/filepath"
	"testing"

	"notes/internal/settings"
)

func TestSwitcherListsVaultsWithCurrentAndMissing(t *testing.T) {
	a := newTestApp(t)
	st, _ := settings.Load(a.settings)
	work, home := st.Recent[0], st.Recent[1]
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	// Home's folder is not made: it is missing.
	c := &Connections{n: a.notebooks, app: a}

	got := c.entries(work)
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].Path != work || !got[0].Current || got[0].Missing || got[0].Name != "Work" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Path != home || got[1].Current || !got[1].Missing {
		t.Fatalf("second = %+v", got[1])
	}

	// A vault open in a window but not remembered is listed first.
	other := filepath.Join(t.TempDir(), "Elsewhere")
	got = c.entries(other)
	if len(got) != 3 || got[0].Path != other || !got[0].Current {
		t.Fatalf("with an unlisted current vault: %+v", got)
	}
}

func TestSwitcherRefusesAMissingVault(t *testing.T) {
	a := newTestApp(t)
	c := &Connections{n: a.notebooks, app: a}
	if err := c.OpenVault(t.Context(), filepath.Join(t.TempDir(), "gone"), false); err == nil {
		t.Fatal("opened a folder that is not there")
	}
}
