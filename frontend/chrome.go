// Window chrome, colors and surfaces adapted from github.com/egoist/kero
// (MIT), through GodGo's frontend/chrome.go, colors.go and elevation.go.
package frontend

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// titleRow is the height of the title bar row, which the sidebar and the
	// page share, and which native windows keep clear of their buttons.
	titleRow = 38
	// nativeBar is the title bar of the native windows (settings, picker), as
	// Kero's and GodGo's: 60 high, its center line at 30, where the traffic
	// lights, the window buttons and the card's title all sit.
	nativeBar = 60
	// cardGap is the room between the window's edge and its card.
	cardGap = 8
	// headerH is the card's title row: the card starts cardGap down, so a row
	// this high has its center on the bar's.
	headerH = 2 * (nativeBar/2 - cardGap)
)

// ownControls is true where the window has no system buttons, so the page
// draws minimize, maximize and close itself (Windows and Linux).
var ownControls = runtime.GOOS != "darwin"

// framed adapts a native window's options to its platform: on macOS the
// traffic lights sit in the title row, elsewhere the window has no buttons of
// its own and the view draws them.
func framed(o mygo.WindowOptions) mygo.WindowOptions {
	if ownControls {
		o.TitleBarStyle = mygo.TitleBarHidden
		o.HideWindowButtons = true
		o.AutoHideMenuBar = true
		return o
	}
	o.TitleBarStyle = mygo.TitleBarHiddenInset
	o.TitleBarHeight = nativeBar
	// The lights are about 16 high and MyGo raises them by 1: their center
	// lands on 30.
	o.TrafficLightPosition = &mygo.Point{X: 18, Y: nativeBar/2 - 7}
	return o
}

// palette is the colors of the native windows, for the light and the dark
// appearance. The values follow Kero's, as GodGo's do.
type palette struct {
	dark bool

	sidebar, text, textMuted, textFaint ui.Color
	rowHover, rowSelected               ui.Color
	card, cardShadow, cardShadow2       ui.Color
	control, controlHover               ui.Color
	controlShadow                       bool
	divider, chip                       ui.Color
	danger, accent, done                ui.Color
}

var lightPalette = palette{
	sidebar: ui.Hex("#eceef4"), text: ui.Hex("#1d1d1f"), textMuted: ui.Hex("#6e6e78"), textFaint: ui.Hex("#a3a3ad"),
	rowHover: ui.RGBA(0, 0, 0, 0.045), rowSelected: ui.Hex("#ffffff"),
	card: ui.Hex("#ffffff"), cardShadow: ui.RGBA(0, 0, 0, 0.07), cardShadow2: ui.RGBA(0, 0, 0, 0.035),
	control: ui.Hex("#ffffff"), controlHover: ui.Hex("#f7f7f9"), controlShadow: true,
	divider: ui.RGBA(0, 0, 0, 0.06), chip: ui.RGBA(0, 0, 0, 0.035),
	danger: ui.Hex("#d93a35"), accent: ui.Hex("#3b6cf6"), done: ui.Hex("#2f9e5b"),
}

var darkPalette = palette{
	dark:    true,
	sidebar: ui.Hex("#141418"), text: ui.Hex("#ececf0"), textMuted: ui.Hex("#a9a9b4"), textFaint: ui.Hex("#83838f"),
	rowHover: ui.RGBA(255, 255, 255, 0.05), rowSelected: ui.RGBA(255, 255, 255, 0.10),
	card: ui.Hex("#232328"), cardShadow: ui.RGBA(0, 0, 0, 0.35), cardShadow2: ui.RGBA(0, 0, 0, 0.2),
	control: ui.Hex("#404047"), controlHover: ui.Hex("#4a4a52"),
	divider: ui.RGBA(255, 255, 255, 0.07), chip: ui.RGBA(255, 255, 255, 0.055),
	danger: ui.Hex("#ff6961"), accent: ui.Hex("#6b93ff"), done: ui.Hex("#5fd28c"),
}

func paletteFor(c *ui.Context) *palette {
	if c.Theme().Dark {
		return &darkPalette
	}
	return &lightPalette
}

// themed sets the theme the base controls draw with to the palette.
func themed(c *ui.Context, pal *palette) {
	t := *c.Theme()
	t.Accent = pal.accent
	t.Selection = pal.accent.Alpha(0.25)
	t.Border = ui.Transparent
	t.Surface = pal.control
	t.SurfaceHover = pal.controlHover
	t.SurfacePressed = pal.controlHover.Mix(pal.text, 0.06)
	t.Text = pal.text
	t.TextMuted = pal.textMuted
	t.FontSize = 13
	c.SetTheme(&t)
}

// backdrop is the window's background behind the card: the sidebar color, or
// the material showing through it when the window is translucent.
func backdrop(c *ui.Context, pal *palette) {
	bg := pal.sidebar
	if c.Vibrancy() {
		bg = bg.Alpha(0.6)
	}
	c.Root().Background(bg)
}

// raised is a surface above the backdrop: a soft shadow in the light, a lit
// hairline in the dark.
func raised(e ui.Element, pal *palette) ui.Element {
	if pal.dark {
		return e.Border(1, pal.text.Alpha(0.12)).BorderLinearGradient(ui.LinearGradient{
			Angle: 180, From: pal.text.Alpha(0.18), To: pal.text.Alpha(0.06),
		}).Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 4, 6, -3, ui.RGBA(0, 0, 0, 0.24))
	}
	return e.Shadow(0, 1, 2, 0, pal.cardShadow).Shadow(0, 1, 6, 0, pal.cardShadow2)
}

// chosen is the selected row of a list.
func chosen(e ui.Element, pal *palette) ui.Element {
	if pal.dark {
		return raised(e, pal)
	}
	return e.Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.06)).Shadow(0, 0.5, 2, 0, ui.RGBA(0, 0, 0, 0.05))
}

// card is the raised surface a window's content sits on, 8 from the window's
// edges and beside its sidebar.
func card(c *ui.Context, pal *palette) ui.Element {
	return raised(ui.Column(c).Grow(1).MinHeight(0).Radius(14).Background(pal.card), pal).Clip()
}

// button is a push button. A primary one is filled with the accent color.
func button(c *ui.Context, key, label string, primary bool, pal *palette) bool {
	b := ui.ButtonBase(c.Key(key)).Height(28).Padding(0, 12).Radius(7).AlignItems(ui.Center).Shrink(0).Label(label)
	bg, fg := pal.control, pal.text
	if primary {
		bg, fg = pal.accent, ui.Hex("#ffffff")
	}
	switch {
	case b.Pressed():
		bg = bg.Mix(ui.Hex("#000000"), 0.08)
	case b.Hovered() && primary:
		bg = bg.Mix(ui.Hex("#ffffff"), 0.08)
	case b.Hovered():
		bg = pal.controlHover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 90 * time.Millisecond})).TextColor(fg)
	if pal.controlShadow && !primary {
		b.Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.13)).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.06))
	} else if !primary {
		raised(b, pal)
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine() })
	return b.Clicked()
}

// toggle is a switch, drawn as Kero draws its own.
func toggle(c *ui.Context, key string, on *bool, label string, pal *palette) ui.Element {
	sw := ui.SwitchBase(c.Key(key), on).Size(38, 22).Radius(11).Padding(2).Shrink(0).Label(label)
	track := pal.text.Alpha(0.14)
	if pal.dark {
		track = ui.RGBA(255, 255, 255, 0.2)
	}
	if *on {
		track = pal.accent
	}
	sw.Background(sw.AnimateColor("track", track, ui.Animation{Duration: 150 * time.Millisecond}))
	x := sw.AnimateTo("knob", b2f(*on)*16, ui.Animation{Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.15}})
	sw.Children(func() {
		ui.Box(c).Size(18, 18).Radius(9).Background(ui.Hex("#ffffff")).Translate(x, 0).
			Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.22))
	})
	return sw
}

// field is the frame of a text input.
func field(frame ui.Element, focused bool, pal *palette) ui.Element {
	frame.Height(28).Padding(0, 8).Gap(6).Radius(7).AlignItems(ui.Center)
	stroke, bg := ui.RGBA(0, 0, 0, 0.13), ui.Hex("#ffffff")
	if pal.dark {
		stroke, bg = ui.RGBA(255, 255, 255, 0.13), ui.RGBA(255, 255, 255, 0.06)
	}
	frame.Background(bg).InsetShadow(0, 0, 0, 1, stroke)
	if glow := frame.Animate("glow", b2f(focused), 120*time.Millisecond); glow > 0 {
		frame.Shadow(0, 0, 0, 3.5*glow, pal.accent.Alpha(0.32*glow)).InsetShadow(0, 0, 0, 1, pal.accent.Alpha(0.75*glow))
	}
	return frame
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// sideTop is where a window's sidebar content starts: below the traffic
// lights on macOS, near the top elsewhere.
func sideTop(c *ui.Context) float32 {
	if ownControls {
		return 12
	}
	return nativeBar
}

// shortPath writes a folder with the home folder as ~.
func shortPath(folder string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		home = filepath.Clean(home)
		if folder == home {
			return "~"
		}
		if strings.HasPrefix(folder, home+string(os.PathSeparator)) {
			return "~" + folder[len(home):]
		}
	}
	return folder
}

// folderName is a vault's name in a list: its folder's name.
func folderName(folder string) string {
	return filepath.Base(folder)
}

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

// Icons from Lucide (ISC), as the web interface uses.
var (
	searchGlyph     = icon(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	generalGlyph    = icon(`<path d="M20 7h-9"/><path d="M14 17H5"/><circle cx="17" cy="17" r="3"/><circle cx="7" cy="7" r="3"/>`)
	appearanceGlyph = icon(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`)
	vaultsGlyph     = icon(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	syncGlyph       = icon(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)
	plugGlyph       = icon(`<path d="M12 22v-5"/><path d="M9 7V2"/><path d="M15 7V2"/><path d="M6 13V8h12v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4Z"/>`)
	aboutGlyph      = icon(`<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`)
	cloudGlyph      = icon(`<path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>`)
	clearGlyph      = icon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
)

var (
	minimizeGlyph = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8h9"/></svg>`))
	maximizeGlyph = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="3.5" width="9" height="9" rx="1.5"/></svg>`))
	restoreGlyph  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="5.5" width="7" height="7" rx="1.2"/><path d="M5.5 3.5h5.5a1.5 1.5 0 0 1 1.5 1.5v5.5"/></svg>`))
	closeGlyph    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg>`))
)

// windowButtons draws minimize, maximize and close for a native window on the
// platforms that have no system buttons: 28×26 each, 2 apart, as Kero's. It
// is empty on macOS, which keeps its traffic lights.
func windowButtons(c *ui.Context, w *mygo.Window, pal *palette) {
	if !ownControls || w == nil {
		return
	}
	plain := func(b ui.Element) {
		if b.Pressed() {
			b.Background(pal.rowHover.Alpha(0.12))
		} else if b.Hovered() {
			b.Background(pal.rowHover).TextColor(pal.text)
		}
	}
	ui.Row(c.Key("window-buttons")).Gap(2).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 6).Children(func() {
		minimize := ui.ButtonBase(c.Key("minimize")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label("Minimize").Tooltip("Minimize")
		plain(minimize)
		minimize.Children(func() { ui.Icon(c, minimizeGlyph).Size(16, 16) })
		if minimize.Clicked() {
			w.Minimize()
		}
		maximized := w.IsMaximized()
		glyph, label := maximizeGlyph, "Maximize"
		if maximized {
			glyph, label = restoreGlyph, "Restore"
		}
		// MaximizeButton lets Windows 11 open its snap layouts over it.
		maxButton := ui.ButtonBase(c.Key("maximize")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(label).MaximizeButton()
		plain(maxButton)
		maxButton.Children(func() { ui.Icon(c, glyph).Size(16, 16) })
		if maxButton.Clicked() {
			if maximized {
				w.Unmaximize()
			} else {
				w.Maximize()
			}
		}
		closeButton := ui.ButtonBase(c.Key("close-window")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label("Close").Tooltip("Close")
		if closeButton.Hovered() || closeButton.Pressed() {
			closeButton.Background(pal.danger).TextColor(ui.RGB(255, 255, 255))
		}
		closeButton.Children(func() { ui.Icon(c, closeGlyph).Size(14, 14) })
		if closeButton.Clicked() {
			w.Close()
		}
	})
}
