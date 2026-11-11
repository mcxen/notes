package frontend

import (
	"context"
	"errors"
	"runtime"

	"github.com/egoist/mygo"

	"notes/internal/settings"
)

// Look is how a vault window is drawn beyond its records: whether its sidebar
// is translucent. The page reads it when it loads.
type Look struct {
	Translucent bool `json:"translucent"`
	// Outline is whether pages show their outline.
	Outline bool `json:"outline"`
}

// Look returns the appearance the settings ask for.
func (n *Notebooks) Look(ctx context.Context) (Look, error) {
	st, err := settings.Load(settingsPath())
	if err != nil {
		return Look{}, err
	}
	return Look{Translucent: translucent(st), Outline: !st.HideOutline}, nil
}

// translucent reports whether the sidebar is translucent: on macOS, unless the
// user turned it off.
func translucent(st settings.Settings) bool {
	return runtime.GOOS == "darwin" && !st.OpaqueSidebar
}

// vibrancyFor is the window material the settings ask for.
func vibrancyFor(st settings.Settings) mygo.Vibrancy {
	if translucent(st) {
		return mygo.VibrancySidebar
	}
	return mygo.VibrancyNone
}

// applyLook gives every open vault window the material the settings now ask
// for, and reloads its page so the page draws the sidebar and the outline to
// match.
func (a *appActions) applyLook() {
	st, err := settings.Load(a.settings)
	if err != nil {
		return
	}
	for _, w := range a.notebooks.windows() {
		w.SetVibrancy(vibrancyFor(st))
		w.Page().Reload()
	}
}

// themeSource reads the theme a page asks for: "system", "light" or "dark".
func themeSource(theme string) (mygo.ThemeSource, bool) {
	switch theme {
	case "system", "":
		return mygo.ThemeSystem, true
	case "light":
		return mygo.ThemeLight, true
	case "dark":
		return mygo.ThemeDark, true
	}
	return "", false
}

// SetTheme makes the app's appearance the theme a window's page shows. The page
// draws itself from the theme it keeps (Light, Dark or the system's), but the
// window's material (the translucent sidebar), the title bar's buttons and the
// native windows follow the app's appearance, which is the system's unless it is
// set: without this a vault kept in Dark under a light system had a dark page
// beside a light sidebar. The window that has the focus decides: a window
// that is not focused (one opened behind the others) is heard only while no
// window has the focus, and says it again when it gets it.
func (n *Notebooks) SetTheme(ctx context.Context, theme string) error {
	source, ok := themeSource(theme)
	if !ok {
		return errors.New("unknown theme " + theme)
	}
	caller := mygo.CallerWindow(ctx)
	if focused := mygo.FocusedWindow(); focused != nil && focused != caller {
		return nil
	}
	mygo.Theme.SetSource(source)
	return nil
}
