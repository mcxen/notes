package frontend

import (
	"context"
	"runtime"

	"github.com/egoist/mygo"

	"notes/internal/settings"
)

// Look is how a vault window is drawn beyond its records: whether its sidebar
// is translucent. The page reads it when it loads.
type Look struct {
	Translucent bool `json:"translucent"`
}

// Look returns the appearance the settings ask for.
func (n *Notebooks) Look(ctx context.Context) (Look, error) {
	st, err := settings.Load(settingsPath())
	if err != nil {
		return Look{}, err
	}
	return Look{Translucent: translucent(st)}, nil
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

// applyTranslucency gives every open vault window the material the settings
// now ask for, and reloads its page so the page draws the sidebar to match.
func (a *appActions) applyTranslucency() {
	st, err := settings.Load(a.settings)
	if err != nil {
		return
	}
	for _, w := range a.notebooks.windows() {
		w.SetVibrancy(vibrancyFor(st))
		w.Page().Reload()
	}
}
