package settings

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Skin is the user's own wallpaper: a background behind the list and one behind
// the page, with a blurred seam between them. Pictures live in SkinsDir.
type Skin struct {
	Sidebar SkinLayer `json:"sidebar,omitzero"`
	Page    SkinLayer `json:"page,omitzero"`
	// Span runs the Sidebar's picture under the page too; Page is then not drawn.
	Span bool `json:"span,omitempty"`
	// History is the pictures kept in SkinsDir, most recently used first.
	History []string `json:"history,omitempty"`
	// SpanVeil is the page's Veil while the picture spans the window.
	SpanVeil int `json:"spanVeil,omitempty"`
	Seam     int `json:"seam,omitempty"`
	SeamBlur int `json:"seamBlur,omitempty"`
}

// SkinLayer is the background of one side of the window.
type SkinLayer struct {
	Kind  string `json:"kind,omitempty"` // "", "image" or "gradient"
	Image string `json:"image,omitempty"`
	From  string `json:"from,omitempty"` // gradient stops, #rrggbb; Via is optional
	Via   string `json:"via,omitempty"`
	To    string `json:"to,omitempty"`
	Angle int    `json:"angle,omitempty"` // degrees, 180 is top to bottom
	Blur  int    `json:"blur,omitempty"`
	// Veil is how much paper is laid over the background so the words stay
	// readable, 0 to 100 percent.
	Veil       int `json:"veil,omitempty"`
	Brightness int `json:"brightness,omitempty"` // -60 to 60
	// Zoom is percent over 100; the offsets move the picture in percent of the
	// room left over.
	Zoom    int `json:"zoom,omitempty"`
	OffsetX int `json:"offsetX,omitempty"`
	OffsetY int `json:"offsetY,omitempty"`
}

// Limits of the numbers a skin keeps.
const (
	MaxBlur     = 60
	MaxSeam     = 160
	MaxSeamBlur = 40
	MaxZoom     = 300
	MaxHistory  = 36
)

const (
	DefaultVeil     = 55
	DefaultSeam     = 72
	DefaultSeamBlur = 18
)

var (
	hexColor  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	skinImage = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.(jpg|jpeg|png|webp|gif|avif|bmp)$`)
)

func ValidSkinImage(name string) bool { return skinImage.MatchString(name) }

func (l SkinLayer) Active() bool { return l.Kind == "image" || l.Kind == "gradient" }

// Clean keeps only numbers, colors and a file name, so a hand-written settings
// file cannot put anything else into a style.
func (l SkinLayer) Clean() SkinLayer {
	switch l.Kind {
	case "image":
		if !ValidSkinImage(l.Image) {
			return SkinLayer{}
		}
	case "gradient":
		if !hexColor.MatchString(l.From) || !hexColor.MatchString(l.To) {
			return SkinLayer{}
		}
		if l.Via != "" && !hexColor.MatchString(l.Via) {
			l.Via = ""
		}
	default:
		return SkinLayer{}
	}
	l.Blur = clamp(l.Blur, 0, MaxBlur)
	l.Veil = clamp(l.Veil, 0, 100)
	l.Brightness = clamp(l.Brightness, -60, 60)
	l.Angle = ((l.Angle % 360) + 360) % 360
	l.Zoom = clamp(l.Zoom, 0, MaxZoom-100)
	l.OffsetX = clamp(l.OffsetX, -100, 100)
	l.OffsetY = clamp(l.OffsetY, -100, 100)
	return l
}

func (s Skin) Clean() Skin {
	s.Sidebar = s.Sidebar.Clean()
	s.Page = s.Page.Clean()
	s.History = cleanHistory(s.History)
	s.SpanVeil = clamp(s.SpanVeil, 0, 100)
	s.Seam = clamp(s.Seam, 0, MaxSeam)
	s.SeamBlur = clamp(s.SeamBlur, 0, MaxSeamBlur)
	return s
}

func (s Skin) Active() bool { return s.Sidebar.Active() || s.Page.Active() }

// Images lists the pictures in use and those in the history.
func (s Skin) Images() []string {
	var names []string
	for _, l := range []SkinLayer{s.Sidebar.Clean(), s.Page.Clean()} {
		if l.Kind == "image" {
			names = append(names, l.Image)
		}
	}
	return append(names, cleanHistory(s.History)...)
}

func cleanHistory(names []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range names {
		if ValidSkinImage(name) && !seen[name] && len(out) < MaxHistory {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func (s *Skin) Remember(name string) {
	if !ValidSkinImage(name) {
		return
	}
	s.History = cleanHistory(append([]string{name}, s.History...))
}

func (s *Skin) Forget(name string) {
	s.History = slices.DeleteFunc(slices.Clone(s.History), func(n string) bool { return n == name })
}

// Start sets a layer's kind, with readable defaults if it had none; a layer
// already set keeps the user's values.
func (l SkinLayer) Start(kind string) SkinLayer {
	if l.Active() {
		l.Kind = kind
	} else {
		l = SkinLayer{Kind: kind, Veil: DefaultVeil}
	}
	if kind == "gradient" && l.From == "" {
		l.From, l.To, l.Angle = "#6d83f2", "#e58bc4", 135
	}
	return l
}

// SkinsDir is beside the settings file.
func SkinsDir(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "skins")
}

// SkinFile is a picture's path, or "" when name is not one the app wrote.
func SkinFile(settingsPath, name string) string {
	if !ValidSkinImage(name) || strings.ContainsAny(name, `/\`) {
		return ""
	}
	return filepath.Join(SkinsDir(settingsPath), name)
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// ThumbFile is the path of a picture's small copy, or "" for an invalid name.
func ThumbFile(settingsPath, name string) string {
	if SkinFile(settingsPath, name) == "" {
		return ""
	}
	return filepath.Join(SkinsDir(settingsPath), "thumbs", strings.TrimSuffix(name, filepath.Ext(name))+".jpg")
}
