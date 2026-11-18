package frontend

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
)

const (
	labelSkinSpan = "One picture across the window"
	labelSkinSeam = "Blurred seam"
)

// wallpaperPage is Settings › Wallpaper; changes show in open windows live.
func (s *settingsWindow) wallpaperPage(c *ui.Context, pal *palette) {
	sk := &s.st.Skin
	changed := false

	s.group(c, pal, tr("List and page"), tr("Choose a picture of your own or a gradient for each side. The picture is copied into Notes' own folder; the original is not touched."), func() {
		span := sk.Span
		s.row(c, pal, tr(labelSkinSpan), tr("The list's picture runs under the page too, as one wallpaper. The page's own background is not drawn."), func() {
			toggle(c, "skin-span", &span, tr(labelSkinSpan), pal)
		})
		if span != sk.Span {
			sk.Span = span
			if span && sk.SpanVeil == 0 {
				sk.SpanVeil = settings.DefaultVeil
			}
			changed = true
		}
		seam := sk.Seam
		if s.sliderRow(c, pal, "skin-seam", tr(labelSkinSeam), tr("How wide the soft blurred band is where the list meets the page. 0 is a plain edge."), &seam, 0, settings.MaxSeam, "px") {
			sk.Seam = seam
			changed = true
		}
		strength := sk.SeamBlur
		if s.sliderRow(c, pal, "skin-seam-blur", tr("Seam blur"), tr("How strongly the band blurs the two backgrounds."), &strength, 0, settings.MaxSeamBlur, "px") {
			sk.SeamBlur = strength
			changed = true
		}
	})

	if s.layerGroup(c, pal, "sidebar", tr("List (left)"), &sk.Sidebar, tr("the list")) {
		changed = true
	}
	if sk.Span && sk.Sidebar.Kind == "image" {
		s.group(c, pal, tr("Page (right)"), "", func() {
			s.row(c, pal, tr("Covered by the one picture"), tr("Turn off “One picture across the window” to give the page a background of its own."), nil)
		})
		// The page's veil still matters: the words are on it.
		s.group(c, pal, "", "", func() {
			if s.slider(c, pal, "page-veil", tr("Veil"), tr("How much of the plain paper is laid over the background of %s, so the words stay easy to read.", tr("the page")), &sk.SpanVeil, 0, 100, "%") {
				changed = true
			}
		})
	} else if s.layerGroup(c, pal, "page", tr("Page (right)"), &sk.Page, tr("the page")) {
		changed = true
	}

	if s.historyGroup(c, pal) {
		changed = true
	}

	s.group(c, pal, "", "", func() {
		s.row(c, pal, tr("Remove the wallpaper"), tr("Back to the plain list and page."), func() {
			if sk.Sidebar.Active() || sk.Page.Active() {
				if button(c, "skin-reset", tr("Remove"), false, pal) {
					*sk = settings.Skin{}
					changed = true
				}
			}
		})
	})

	if changed {
		s.save()
		pruneSkinImages(s.a.settings, *sk)
		s.a.applySkin()
	}
}

// layerGroup is one side's settings; it reports whether anything changed.
func (s *settingsWindow) layerGroup(c *ui.Context, pal *palette, side, title string, l *settings.SkinLayer, what string) (changed bool) {
	s.group(c, pal, title, "", func() {
		kind := map[string]int{"": 0, "image": 1, "gradient": 2}[l.Kind]
		picked := kind
		s.row(c, pal, tr("Background"), tr("Of %s.", what), func() {
			ui.Segmented(c.Key("skin-kind-"+side), &picked, tr("None"), tr("Picture"), tr("Gradient")).Label(tr("Background"))
		})
		if picked != kind {
			switch picked {
			case 0:
				*l = settings.SkinLayer{}
				changed = true
			case 1:
				// A picture has to be chosen before there is a background: the
				// dialog opens, and the layer is set when it is answered.
				s.pickPicture(side)
			case 2:
				s.startSeam()
				*l = l.Start("gradient")
				changed = true
			}
		}
		switch l.Kind {
		case "image":
			s.row(c, pal, tr("Picture"), s.pictureName(l), func() {
				if l.Image != "" {
					s.pictureTile(c, pal, "skin-now-"+side, l.Image, 64, 40, false)
				}
				if button(c, "skin-pick-"+side, tr("Choose…"), true, pal) {
					s.pickPicture(side)
				}
			})
			if s.slider(c, pal, side+"-zoom", tr("Zoom"), tr("Enlarge the picture of %s to show part of it.", what), &l.Zoom, 0, settings.MaxZoom-100, "%") {
				changed = true
			}
			if s.slider(c, pal, side+"-x", tr("Move across"), tr("Slide the picture of %s sideways, to choose which part shows.", what), &l.OffsetX, -100, 100, "") {
				changed = true
			}
			if s.slider(c, pal, side+"-y", tr("Move up or down"), tr("Slide the picture of %s up or down, to choose which part shows.", what), &l.OffsetY, -100, 100, "") {
				changed = true
			}
		case "gradient":
			if s.gradientRows(c, pal, side, l, what) {
				changed = true
			}
		}
		if l.Active() {
			if s.slider(c, pal, side+"-blur", tr("Blur"), tr("Soften the background of %s.", what), &l.Blur, 0, settings.MaxBlur, "px") {
				changed = true
			}
			if s.slider(c, pal, side+"-bright", tr("Brightness"), tr("Darken or lighten the background of %s.", what), &l.Brightness, -60, 60, "") {
				changed = true
			}
			if s.veilRow(c, pal, side, l, what) {
				changed = true
			}
		}
	})
	return changed
}

func (s *settingsWindow) veilRow(c *ui.Context, pal *palette, side string, l *settings.SkinLayer, what string) bool {
	if !l.Active() {
		return false
	}
	var changed bool
	s.group(c, pal, "", "", func() {
		changed = s.slider(c, pal, side+"-veil", tr("Veil"), tr("How much of the plain paper is laid over the background of %s, so the words stay easy to read.", what), &l.Veil, 0, 100, "%")
	})
	return changed
}

func (s *settingsWindow) gradientRows(c *ui.Context, pal *palette, side string, l *settings.SkinLayer, what string) (changed bool) {
	s.row(c, pal, tr("Colors"), tr("From, through (optional) and to, along the direction below, for %s.", what), func() {
		from, to := ui.Hex(l.From), ui.Hex(l.To)
		ui.ColorWell(c.Key("skin-from-"+side), &from).Label(tr("From")).Size(30, 22)
		hasVia := l.Via != ""
		via := ui.Hex("#a78bfa")
		if hasVia {
			via = ui.Hex(l.Via)
			ui.ColorWell(c.Key("skin-via-"+side), &via).Label(tr("Through")).Size(30, 22)
			if button(c, "skin-novia-"+side, "−", false, pal) {
				l.Via = ""
				changed = true
			}
		} else if button(c, "skin-addvia-"+side, "+", false, pal) {
			l.Via = hexOf(via)
			changed = true
		}
		ui.ColorWell(c.Key("skin-to-"+side), &to).Label(tr("To")).Size(30, 22)
		if h := hexOf(from); h != l.From {
			l.From, changed = h, true
		}
		if h := hexOf(to); h != l.To {
			l.To, changed = h, true
		}
		if hasVia {
			if h := hexOf(via); h != l.Via {
				l.Via, changed = h, true
			}
		}
	})
	if s.slider(c, pal, side+"-angle", tr("Direction"), tr("The angle of the gradient of %s; 180 is top to bottom.", what), &l.Angle, 0, 359, "°") {
		changed = true
	}
	return changed
}

// hexOf is a color as #rrggbb, ignoring opacity.
func hexOf(col ui.Color) string { return fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B) }

func (s *settingsWindow) pictureName(l *settings.SkinLayer) string {
	if l.Image == "" {
		return tr("No picture chosen.")
	}
	return tr("A picture is set. Choose another to replace it.")
}

// pickPicture opens the dialog; the layer is set when it is answered.
func (s *settingsWindow) pickPicture(side string) {
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      s.win,
			Title:       tr("Choose a picture"),
			ButtonLabel: tr("Choose"),
			DefaultPath: defaultPictureFolder(),
			Filters:     []mygo.FileFilter{{Name: tr("Pictures"), Extensions: []string{"jpg", "jpeg", "png", "webp", "gif", "avif", "bmp"}}},
		})
		if err != nil || len(files) == 0 {
			return
		}
		name, err := importSkinImage(s.a.settings, files[0])
		s.update(func() {
			if err != nil {
				s.notes = err.Error()
				return
			}
			layer := &s.st.Skin.Sidebar
			if side == "page" {
				layer = &s.st.Skin.Page
			}
			s.startSeam()
			*layer = layer.Start("image")
			layer.Image = name
			s.st.Skin.Remember(name)
			s.save()
			pruneSkinImages(s.a.settings, s.st.Skin)
			s.a.applySkin()
		})
	}()
}

// startSeam gives the first background a default seam.
func (s *settingsWindow) startSeam() {
	if !s.st.Skin.Active() {
		s.st.Skin.Seam, s.st.Skin.SeamBlur = settings.DefaultSeam, settings.DefaultSeamBlur
	}
}

func defaultPictureFolder() string {
	if dir, err := mygo.App.Path(mygo.PathPictures); err == nil {
		return dir
	}
	return filepath.Dir(defaultLocation())
}

// slider reports whether the user moved it.
func (s *settingsWindow) slider(c *ui.Context, pal *palette, key, label, detail string, v *int, lo, hi int, unit string) bool {
	return s.sliderRow(c, pal, "skin-"+key, label, detail, v, lo, hi, unit)
}

func (s *settingsWindow) sliderRow(c *ui.Context, pal *palette, key, label, detail string, v *int, lo, hi int, unit string) (moved bool) {
	value := float64(*v)
	s.row(c, pal, label, detail, func() {
		ui.Slider(c.Key(key), &value, float64(lo), float64(hi)).Width(170).Step(1).Label(label)
		ui.Text(c, fmt.Sprintf("%d%s", *v, unit)).FontSize(12).TextColor(pal.textMuted).Width(44).TextAlign(ui.End)
	})
	if n := int(math.Round(value)); n != *v {
		*v = n
		moved = true
	}
	return moved
}

// thumb is a picture's small copy, read once; nil when it has none.
func (s *settingsWindow) thumb(name string) *ui.Bitmap {
	if b, ok := s.thumbs[name]; ok {
		return b
	}
	if s.thumbs == nil {
		s.thumbs = map[string]*ui.Bitmap{}
	}
	var b *ui.Bitmap
	if data, err := os.ReadFile(settings.ThumbFile(s.a.settings, name)); err == nil {
		b, _ = ui.DecodeBitmap(data)
	}
	s.thumbs[name] = b
	return b
}

// pictureTile is a small picture button with a larger hover preview.
func (s *settingsWindow) pictureTile(c *ui.Context, pal *palette, key, name string, w, h float32, current bool) bool {
	tile := ui.ButtonBase(c.Key(key)).Size(w, h).Radius(7).Shrink(0).Clip().Label(tr("Wallpaper")).Background(pal.chip)
	if current {
		tile.Border(2, pal.accent)
	} else {
		tile.Border(1, pal.divider)
	}
	small := s.thumb(name)
	tile.Children(func() {
		if small != nil {
			ui.Image(c, small).Size(w, h).Fit(ui.Cover)
		} else {
			ui.Text(c, strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))).FontSize(11).TextColor(pal.textFaint)
		}
	})
	if small != nil {
		bw, bh := ui.ImageSize(small)
		const width = 380
		ui.TooltipBase(c, tile, func(tip ui.Element) {
			tip.Padding(5).Radius(12).Background(pal.card).Shadow(0, 6, 22, 0, ui.RGBA(0, 0, 0, 0.35)).Border(1, pal.divider)
			tip.Children(func() {
				ui.Image(c, small).Size(width, min(width*bh/max(bw, 1), 300)).Fit(ui.Contain).Radius(8)
			})
		})
	}
	return tile.Clicked()
}

// historyGroup lists earlier pictures to use again; it reports whether the skin changed.
func (s *settingsWindow) historyGroup(c *ui.Context, pal *palette) (changed bool) {
	sk := &s.st.Skin
	s.group(c, pal, tr("History"), tr("Pictures you have used, kept in Notes' own folder. Point at one to see it larger; press it to use it."), func() {
		if len(sk.History) == 0 {
			s.row(c, pal, tr("No pictures yet"), tr("A picture you choose is kept here, to choose again."), nil)
			return
		}
		if !(sk.Span && sk.Sidebar.Kind == "image") {
			s.row(c, pal, tr("Use it for"), "", func() {
				ui.Segmented(c.Key("skin-target"), &s.skinTarget, tr("List"), tr("Page")).Label(tr("Use it for"))
			})
		}
		ui.Row(c.Key("skin-history")).Wrap().Gap(12).Padding(10, 0).Children(func() {
			for _, name := range slices.Clone(sk.History) {
				name := name
				ui.Column(c.Key("hist-" + name)).Gap(4).AlignItems(ui.Center).Children(func() {
					current := sk.Sidebar.Image == name && sk.Sidebar.Kind == "image" || sk.Page.Image == name && sk.Page.Kind == "image"
					if s.pictureTile(c, pal, "hist-use-"+name, name, 104, 66, current) {
						layer := &sk.Sidebar
						if s.skinTarget == 1 && !(sk.Span && sk.Sidebar.Kind == "image") {
							layer = &sk.Page
						}
						s.startSeam()
						*layer = layer.Start("image")
						layer.Image = name
						sk.Remember(name)
						changed = true
					}
					rm := ui.ButtonBase(c.Key("hist-rm-"+name)).Height(18).Padding(0, 6).Radius(5).Label(tr("Remove")).
						Background(ui.Transparent).TextColor(pal.textFaint)
					if rm.Hovered() {
						rm.TextColor(pal.danger)
					}
					rm.Children(func() { ui.Text(c, tr("Remove")).FontSize(11) })
					if rm.Clicked() {
						sk.Forget(name)
						delete(s.thumbs, name)
						changed = true
					}
				})
			}
		})
	})
	return changed
}
