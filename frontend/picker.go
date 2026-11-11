package frontend

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// picker is the vault picker: the vaults opened before, and the ways to add one.
type picker struct {
	a      *appActions
	recent []string
	win    *mygo.Window

	// The form of a new vault, shown when there is no vault yet and when New
	// Vault is chosen: where it goes, what it is called, and what is wrong.
	creating bool
	location string
	name     string
	autoName string // the name offered, so that a name typed is never replaced
	problem  string
	busy     bool
}

// newPicker is a picker that offers the form of a new vault when there is no
// vault to open.
func newPicker(a *appActions) *picker {
	p := &picker{a: a, recent: a.recentFolders()}
	p.startForm()
	p.creating = len(p.recent) == 0
	return p
}

// startForm offers the place and the name a new vault gets unless they are changed.
func (p *picker) startForm() {
	p.location = shortPath(defaultLocation())
	p.autoName = uniqueName(defaultLocation(), defaultBaseName())
	p.name = p.autoName
	p.problem = ""
}

// chooseLocation asks for the folder the new vault goes in, from the one
// shown. The first time, at launch, it is also how the app is given the right to
// the folder: the system lets it into one the person picked.
func (p *picker) chooseLocation(message string) {
	start := expandHome(p.location)
	if !isDirectory(start) {
		start = defaultLocation()
	}
	folders, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            p.win,
		Title:             tr("Choose where to keep the new vault"),
		Message:           message,
		DefaultPath:       start,
		Directory:         true,
		CreateDirectories: true,
		ButtonLabel:       tr("Choose"),
	})
	if err != nil || len(folders) == 0 {
		return
	}
	p.location = shortPath(folders[0])
	// A name that was only offered follows the place; one typed stays.
	if p.name == p.autoName {
		p.autoName = uniqueName(folders[0], defaultBaseName())
		p.name = p.autoName
	}
	p.problem = ""
}

// create makes the vault the form describes, opens it and closes the picker.
func (p *picker) create() {
	if p.busy {
		return
	}
	p.busy = true
	defer func() { p.busy = false }()
	path, err := createVaultFolder(p.location, p.name)
	if err != nil {
		p.problem = err.Error()
		return
	}
	// A vault made here opens on a page of its own, not on an empty window.
	if err := seedWelcomePage(path); err != nil {
		log.Printf("notes: first page: %v", err)
	}
	if err := p.a.open(path); err != nil {
		_ = os.RemoveAll(path) // made a moment ago, by this window
		p.problem = tr("The folder could not be opened as a vault: %v", err)
		return
	}
	if p.win != nil {
		p.win.Close()
	}
}

// showPicker opens the picker, or brings the open one forward.
func (a *appActions) showPicker() {
	if a.pickerWin != nil && !a.pickerWin.IsDestroyed() && a.pickerView != nil {
		a.pickerView.recent = a.recentFolders()
		a.pickerWin.Show()
		a.pickerWin.Focus()
		return
	}
	p := newPicker(a)
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
	if p.creating && len(p.recent) == 0 {
		// The first time, there is nothing to pick from: ask for the place at once,
		// starting at Documents, rather than leave the person to find the button.
		go func() {
			time.Sleep(250 * time.Millisecond)
			p.chooseLocation(tr("Notes keeps a vault as a folder of its own. Choose where it goes: Documents is a good place."))
		}()
	}
}

// showNewVault opens the picker on the form of a new vault.
func (a *appActions) showNewVault() {
	a.showPicker()
	if p := a.pickerView; p != nil {
		if !p.creating {
			p.startForm()
		}
		p.creating = true
	}
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
				if p.creating {
					ui.Text(c, tr("Create a vault")).FontSize(20).FontWeight(650).TextColor(pal.text)
					ui.Text(c, tr("A vault is a folder of notes. Choose where it goes and what it is called.")).FontSize(13).TextColor(pal.textMuted)
				} else {
					ui.Text(c, tr("Choose a vault")).FontSize(20).FontWeight(650).TextColor(pal.text)
					ui.Text(c, tr("A vault is a folder of notes. Open one you used before, or start a new one.")).FontSize(13).TextColor(pal.textMuted)
				}
			})
			if p.creating {
				p.form(c, pal)
			} else {
				p.list(c, pal)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if button(c, "connect", tr("Connect to a Remote Folder…"), false, pal) {
					go p.a.connect()
				}
				ui.Spacer(c)
				if p.creating {
					if len(p.recent) > 0 && button(c, "back", tr("Back"), false, pal) {
						p.creating = false
					}
					if button(c, "open", tr("Open Folder…"), false, pal) {
						go p.a.openVault()
					}
					if button(c, "create", tr("Create Vault"), true, pal) {
						go p.create()
					}
					return
				}
				if button(c, "open", tr("Open Folder…"), false, pal) {
					go p.a.openVault()
				}
				if button(c, "new", tr("New Vault…"), true, pal) {
					p.startForm()
					p.creating = true
				}
			})
		})
	})
}

// labeled is a form row: its label above, then what it holds.
func labeled(c *ui.Context, pal *palette, key, label string, body func()) {
	ui.Column(c.Key(key)).Gap(5).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(pal.textMuted)
		body()
	})
}

// form is the new vault's place and name, each a text that can be changed, and
// what is wrong with them.
func (p *picker) form(c *ui.Context, pal *palette) {
	ui.Column(c.Key("form")).Grow(1).MinHeight(0).Gap(14).Children(func() {
		labeled(c, pal, "where", tr("Location"), func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				var in ui.Element
				frame := ui.Row(c.Key("loc")).Grow(1).MinWidth(0).Cursor(ui.CursorText)
				frame.Children(func() {
					in = ui.TextInputBase(c.Key("loc-in"), &p.location).Grow(1).MinWidth(0).FontSize(13).
						Placeholder(tr("Location")).Label(tr("Location"))
					in.OnShortcut(0, ui.KeyEnter, func() { go p.create() })
				})
				field(frame, in.Focused(), pal)
				if button(c, "choose", tr("Choose…"), false, pal) {
					go p.chooseLocation(tr("Choose where the new vault goes."))
				}
			})
		})
		labeled(c, pal, "what", tr("Name"), func() {
			var in ui.Element
			frame := ui.Row(c.Key("nm")).Cursor(ui.CursorText)
			frame.Children(func() {
				in = ui.TextInputBase(c.Key("nm-in"), &p.name).Grow(1).MinWidth(0).FontSize(13).
					Placeholder(tr("Name")).Label(tr("Name"))
				in.OnShortcut(0, ui.KeyEnter, func() { go p.create() })
			})
			field(frame, in.Focused(), pal)
		})
		ui.Text(c, tr("The vault is the folder %s.", shortPath(filepath.Join(expandHome(p.location), strings.TrimSpace(p.name))))).
			FontSize(12).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
		if p.problem != "" {
			ui.Text(c, p.problem).FontSize(12.5).TextColor(pal.danger)
		}
		ui.Spacer(c)
	})
}

// list is the vaults opened before.
func (p *picker) list(c *ui.Context, pal *palette) {
	ui.Scroll(c.Key("recent")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).AlignItems(ui.Stretch).Gap(2).Children(func() {
			if len(p.recent) == 0 {
				ui.Text(c, tr("No vaults yet.")).FontSize(13).TextColor(pal.textFaint).Padding(10, 12)
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
}
