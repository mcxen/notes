package frontend

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// picker is the vault picker: the vaults opened before, and the ways to add one.
type picker struct {
	a      *appActions
	recent []string
	win    *mygo.Window
}

// showPicker opens the picker, or brings the open one forward.
func (a *appActions) showPicker() {
	if a.pickerWin != nil && !a.pickerWin.IsDestroyed() && a.pickerView != nil {
		a.pickerView.recent = a.recentFolders()
		a.pickerWin.Show()
		a.pickerWin.Focus()
		return
	}
	p := &picker{a: a, recent: a.recentFolders()}
	a.pickerView = p
	w := mygo.NewWindow(framed(mygo.WindowOptions{
		Title:     appName,
		Width:     560,
		Height:    460,
		MinWidth:  460,
		MinHeight: 380,
		StateKey:  "picker",
		Content:   ui.View(p.view),
	}))
	p.win = w
	a.pickerWin = w
	w.OnFocus(func() { p.recent = a.recentFolders() })
	w.OnClosed(func() { a.pickerWin = nil })
}

func (p *picker) view(c *ui.Context) {
	pal := paletteFor(c)
	themed(c, pal)
	backdrop(c, pal)
	ui.Column(c).Fill().Padding(0, cardGap, cardGap, cardGap).Children(func() {
		// The title row: the traffic lights on macOS, the window buttons elsewhere.
		ui.Row(c.Key("title")).Height(nativeBar - cardGap).Shrink(0).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Spacer(c)
			windowButtons(c, p.win, pal)
		})
		card(c, pal).Padding(22, 24, 20, 24).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Choose a vault").FontSize(20).FontWeight(650).TextColor(pal.text)
				ui.Text(c, "A vault is a folder of notes. Open one you used before, or start a new one.").FontSize(13).TextColor(pal.textMuted)
			})
			ui.Scroll(c.Key("recent")).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).AlignItems(ui.Stretch).Gap(2).Children(func() {
					if len(p.recent) == 0 {
						ui.Text(c, "No vaults yet.").FontSize(13).TextColor(pal.textFaint).Padding(10, 12)
					}
					for _, folder := range p.recent {
						row := ui.ButtonBase(c.Key("vault:"+folder)).Padding(8, 10).Gap(12).Radius(9).AlignItems(ui.Center).Label(p.a.recentLabel(folder))
						bg := ui.Transparent
						if row.Hovered() {
							bg = pal.rowHover
						}
						row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))
						row.Children(func() {
							glyph := vaultsGlyph
							if _, remote := remoteOf(folder); remote {
								glyph = cloudGlyph
							}
							ui.Box(c).Size(32, 32).Radius(8).Center().Shrink(0).Background(pal.chip).Children(func() {
								ui.Icon(c, glyph).Size(16, 16).TextColor(pal.textMuted)
							})
							ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
								ui.Text(c, p.a.recentLabel(folder)).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…")
								ui.Text(c, shortPath(folder)).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
							})
						})
						if row.Clicked() {
							go p.a.openFromPicker(p, folder)
						}
					}
				})
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if button(c, "connect", "Connect to a Remote Folder…", false, pal) {
					go p.a.connect()
				}
				ui.Spacer(c)
				if button(c, "open", "Open Folder…", false, pal) {
					go p.a.openVault()
				}
				if button(c, "new", "New Vault…", true, pal) {
					go p.a.newVault()
				}
			})
		})
	})
}
