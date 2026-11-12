package frontend

import "testing"

func TestQuickNewPageGoesToTheWindowInFrontFirst(t *testing.T) {
	if got := quickTarget([]string{"/a", "/b"}, []string{"/c"}); got != "/a" {
		t.Errorf("a visible window: %q", got)
	}
	if got := quickTarget(nil, []string{"/c", "/d"}); got != "/c" {
		t.Errorf("no window, the last vault: %q", got)
	}
	if got := quickTarget(nil, nil); got != "" {
		t.Errorf("no vault at all: %q", got)
	}
}

func TestDockHidesOnlyWhenAskedWithTheTrayAndNoWindow(t *testing.T) {
	cases := []struct {
		hide, tray, visible, want bool
	}{
		{true, true, false, true},   // asked, tray shown, nothing on screen
		{true, true, true, false},   // a window is up: the Dock shows
		{false, true, false, false}, // not asked
		{true, false, false, false}, // no tray: the Dock is the only way back
	}
	for _, c := range cases {
		if got := dockHidden(c.hide, c.tray, c.visible); got != c.want {
			t.Errorf("dockHidden(%v,%v,%v) = %v, want %v", c.hide, c.tray, c.visible, got, c.want)
		}
	}
}
