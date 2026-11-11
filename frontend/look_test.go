package frontend

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestThemeSourceReadsWhatPagesSend(t *testing.T) {
	for theme, want := range map[string]mygo.ThemeSource{
		"system": mygo.ThemeSystem, "": mygo.ThemeSystem, "light": mygo.ThemeLight, "dark": mygo.ThemeDark,
	} {
		got, ok := themeSource(theme)
		if !ok || got != want {
			t.Errorf("themeSource(%q) = %q, %v; want %q", theme, got, ok, want)
		}
	}
	if _, ok := themeSource("sepia"); ok {
		t.Error("a theme the app does not have was accepted")
	}
}
