// Settings window adapted from github.com/egoist/kero (MIT), through GodGo's
// settings_view.go: a sidebar of pages with a search field, and the page on a
// raised card, its settings in grouped rows.
package frontend

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
	vsync "notes/internal/sync"
	"notes/internal/synclog"
)

// settingsPage is one page of the settings window.
type settingsPage struct {
	id, title string
	glyph     *ui.SVG
}

var settingsPages = []settingsPage{
	{"general", "General", generalGlyph},
	{"appearance", "Appearance", appearanceGlyph},
	{"vaults", "Vaults", vaultsGlyph},
	{"sync", "Sync", syncGlyph},
	{"about", "About", aboutGlyph},
}

// settingsWindow is the settings window. Its state is read from disk when it
// opens and after each change, so the views only draw what was loaded.
type settingsWindow struct {
	a     *appActions
	win   *mygo.Window
	page  string
	st    settings.Settings
	log   []synclog.Entry
	trash map[string][]vsync.TrashItem
	notes string
	toast string

	query     string
	searchRef ui.Ref
	hits      map[string]bool
	rows      int
}

// openSettings opens the settings window on a page, or brings it forward.
func (a *appActions) openSettings(page string) {
	if a.settingsWin != nil && !a.settingsWin.IsDestroyed() {
		if page != "" {
			a.settingsView.page = page
		}
		a.settingsView.refresh()
		a.settingsWin.Show()
		a.settingsWin.Focus()
		return
	}
	s := &settingsWindow{a: a, page: "general"}
	if page != "" {
		s.page = page
	}
	s.refresh()
	st, _ := settings.Load(a.settings)
	s.win = mygo.NewWindow(framed(mygo.WindowOptions{
		Title:     "Settings",
		Width:     820,
		Height:    580,
		MinWidth:  680,
		MinHeight: 440,
		StateKey:  "settings",
		Vibrancy:  vibrancyFor(st),
		Content:   ui.View(s.view),
	}))
	a.settingsWin = s.win
	a.settingsView = s
	s.win.OnFocus(func() { s.refresh() })
	s.win.OnClosed(func() {
		a.settingsWin = nil
		s.query = ""
	})
}

// refresh reads the settings, the sync log and the trash of each remote vault.
func (s *settingsWindow) refresh() {
	st, err := settings.Load(s.a.settings)
	if err == nil {
		s.st = st
	}
	entries, err := syncLog().Entries()
	if err == nil {
		s.log = entries
	}
	s.trash = map[string][]vsync.TrashItem{}
	for _, folder := range s.st.Recent {
		if _, ok := s.st.RemoteOf(folder); ok {
			items, _ := vsync.ListTrash(folder)
			if len(items) > 8 {
				items = items[:8]
			}
			s.trash[folder] = items
		}
	}
}

func (s *settingsWindow) save() {
	if err := s.st.Save(s.a.settings); err != nil {
		s.notes = "The settings could not be saved: " + err.Error()
		return
	}
	s.notes = ""
}

func (s *settingsWindow) view(c *ui.Context) {
	pal := paletteFor(c)
	themed(c, pal)
	backdrop(c, pal)
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
	found := s.search()
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { s.searchRef.Focus(c) })
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c.Key("nav")).Width(212).Shrink(0).Padding(sideTop(c), 10, 12, 10).Gap(2).DragWindow().Children(func() {
			s.searchField(c, pal)
			for _, p := range settingsPages {
				if found == nil || found[p.id] != nil {
					s.navItem(c, pal, p)
				}
			}
			if found != nil && len(found) == 0 {
				ui.Text(c, "No settings match").FontSize(13).TextColor(pal.textFaint).Padding(8, 10)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Padding(cardGap, cardGap, cardGap, 0).Children(func() {
			card(c, pal).Children(func() {
				page := s.current()
				ui.Row(c.Key("header")).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 10, 0, 24).DragWindow().Children(func() {
					ui.Text(c, page.title).FontSize(15).FontWeight(650).TextColor(pal.text)
					ui.Spacer(c)
					windowButtons(c, s.win, pal)
				})
				ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
				ui.Scroll(c.Key("page-"+page.id)).Grow(1).MinHeight(0).Padding(18, 24, 28, 24).Children(func() {
					ui.Column(c).Gap(22).MaxWidth(620).Children(func() {
						switch page.id {
						case "appearance":
							s.appearancePage(c, pal)
						case "vaults":
							s.vaultsPage(c, pal)
						case "sync":
							s.syncPage(c, pal)
						case "about":
							s.aboutPage(c, pal)
						default:
							s.generalPage(c, pal)
						}
						if s.notes != "" {
							ui.Text(c, s.notes).FontSize(12).TextColor(pal.danger).Padding(0, 4)
						}
					})
				})
			})
		})
	})
}

func (s *settingsWindow) current() settingsPage {
	for _, p := range settingsPages {
		if p.id == s.page {
			return p
		}
	}
	return settingsPages[0]
}

func (s *settingsWindow) navItem(c *ui.Context, pal *palette, p settingsPage) {
	sel := s.page == p.id
	item := ui.Row(c.Key(p.id)).Height(32).Padding(0, 10).Gap(10).AlignItems(ui.Center).Radius(8).Label(p.title)
	if pal.dark {
		item.Border(1, ui.Transparent)
	}
	bg := ui.Transparent
	switch {
	case sel:
		bg = pal.rowSelected
		chosen(item, pal)
	case item.Hovered():
		bg = pal.rowHover
	}
	item.Background(item.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))
	if (item.Pressed() || item.Clicked()) && !sel {
		s.page = p.id
	}
	item.Children(func() {
		glyph := ui.Icon(c, p.glyph).Size(15, 15).TextColor(pal.textMuted)
		if sel {
			glyph.TextColor(pal.accent)
		}
		ui.Text(c, p.title).FontSize(13.5).TextColor(pal.text).SingleLine()
	})
}

func (s *settingsWindow) searchField(c *ui.Context, pal *palette) {
	var in ui.Element
	frame := ui.Row(c.Key("search")).Cursor(ui.CursorText).Margin(0, 0, 10, 0)
	frame.Children(func() {
		ui.Icon(c, searchGlyph).Size(13, 13).TextColor(pal.textFaint).Shrink(0)
		in = ui.TextInputBase(c.Key("in"), &s.query).Bind(&s.searchRef).Grow(1).MinWidth(0).FontSize(13).
			Placeholder("Search settings").Label("Search settings")
		in.OnShortcut(0, ui.KeyEscape, func() { s.query = "" })
		if s.query != "" {
			x := ui.ButtonBase(c.Key("clear")).Size(16, 16).Radius(8).Center().Shrink(0).Label("Clear search").
				Background(pal.textFaint.Alpha(0.55)).TextColor(pal.card)
			x.Children(func() { ui.Icon(c, clearGlyph).Size(9, 9) })
			if x.Clicked() {
				s.query = ""
				s.searchRef.Focus(c)
			}
		}
	})
	field(frame, in.Focused(), pal)
}

// searchEntry is a setting the search finds: its page, its label and the
// other words it answers to.
type searchEntry struct{ page, label, words string }

var searchEntries = []searchEntry{
	{"general", labelPickerAtLaunch, "startup launch open picker last vault"},
	{"appearance", labelTranslucent, "translucent vibrancy material transparent glass"},
	{"vaults", "Recent vaults", "vault folder recent remove show finder explorer"},
	{"vaults", "Add a vault", "new open folder"},
	{"sync", "Remote vaults", "sync webdav smb remote cloud disconnect"},
	{"sync", "Recent syncs", "log history conflicts trash restore"},
	{"about", "Version", "version about update"},
	{"about", "Settings file", "settings file json config path"},
}

// search finds the pages and rows that match the query, and moves to the
// first page with a match when the current one has none. It is nil without a
// query.
func (s *settingsWindow) search() map[string][]string {
	terms := strings.Fields(strings.ToLower(s.query))
	s.hits = nil
	if len(terms) == 0 {
		return nil
	}
	found := map[string][]string{}
	for _, p := range settingsPages {
		if matches(p.title, terms) {
			found[p.id] = []string{}
		}
	}
	for _, e := range searchEntries {
		if matches(e.label+" "+e.words, terms) {
			found[e.page] = append(found[e.page], e.label)
		}
	}
	if found[s.page] == nil {
		for _, p := range settingsPages {
			if found[p.id] != nil {
				s.page = p.id
				break
			}
		}
	}
	s.hits = map[string]bool{}
	for _, label := range found[s.page] {
		s.hits[label] = true
	}
	return found
}

func matches(text string, terms []string) bool {
	text = strings.ToLower(text)
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// group is a titled box of setting rows, with a note under it.
func (s *settingsWindow) group(c *ui.Context, pal *palette, title, note string, rows func()) {
	ui.Column(c).Gap(8).Children(func() {
		if title != "" {
			t := ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(pal.textMuted).Padding(0, 4)
			if s.hits[title] {
				t.TextColor(pal.accent)
			}
		}
		s.rows = 0
		ui.Column(c).Radius(12).Background(pal.chip).Padding(2, 16).Children(rows)
		if note != "" {
			ui.Text(c, note).FontSize(12).TextColor(pal.textFaint).LineHeight(1.4).Padding(0, 4)
		}
	})
}

// row is one setting: its label and detail on the left, its controls on the
// right. Rows after the first in a group have a hairline above them.
func (s *settingsWindow) row(c *ui.Context, pal *palette, label, detail string, controls func()) {
	if s.rows++; s.rows > 1 {
		ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
	}
	r := ui.Row(c.Key("row-"+label+"\x00"+detail)).Padding(10, 0).Gap(16).AlignItems(ui.Center).MinHeight(44)
	if s.hits[label] {
		r.Background(pal.accent.Alpha(0.13)).Radius(8)
	}
	r.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, label).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…")
			if detail != "" {
				line := ui.Text(c, detail).FontSize(12).TextColor(pal.textMuted).LineHeight(1.35).MinWidth(0)
				if strings.ContainsAny(detail, `/\`) {
					line.SingleLine().Ellipsis("…").Tooltip(detail)
				}
			}
		})
		if controls != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(controls)
		}
	})
}

const (
	labelPickerAtLaunch = "Show the vault picker when Notes opens"
	labelTranslucent    = "Translucent sidebar"
)

func (s *settingsWindow) generalPage(c *ui.Context, pal *palette) {
	s.group(c, pal, "Startup", "", func() {
		picker := s.st.PickerAtLaunch
		s.row(c, pal, labelPickerAtLaunch, "When it is off, Notes reopens the vault you used last.", func() {
			toggle(c, "picker-at-launch", &picker, labelPickerAtLaunch, pal)
		})
		if picker != s.st.PickerAtLaunch {
			s.st.PickerAtLaunch = picker
			s.save()
		}
	})
	s.group(c, pal, "Shortcuts", "", func() {
		mod := "⌘"
		if runtime.GOOS != "darwin" {
			mod = "Ctrl+"
		}
		for _, k := range [][2]string{
			{"Open a vault", mod + "O"},
			{"Vault picker", mod + "⇧O"},
			{"Sync now", mod + "⇧S"},
			{"Toggle the sidebar", mod + "\\"},
			{"Settings", mod + ","},
		} {
			keys := k[1]
			if runtime.GOOS != "darwin" {
				keys = strings.ReplaceAll(keys, "⇧", "Shift+")
			}
			s.row(c, pal, k[0], "", func() {
				ui.Text(c, keys).FontSize(12.5).TextColor(pal.textMuted).Padding(3, 8).Radius(6).Background(pal.chip)
			})
		}
	})
}

func (s *settingsWindow) appearancePage(c *ui.Context, pal *palette) {
	if runtime.GOOS != "darwin" {
		s.group(c, pal, "Window", "", func() {
			s.row(c, pal, labelTranslucent, "Available on macOS.", nil)
		})
		return
	}
	s.group(c, pal, "Window", "Light and dark follow the system, or the theme you pick in a vault's sidebar menu.", func() {
		translucent := !s.st.OpaqueSidebar
		s.row(c, pal, labelTranslucent, "The sidebar shows the desktop through it, as Mac apps do.", func() {
			toggle(c, "translucent", &translucent, labelTranslucent, pal)
		})
		if translucent == s.st.OpaqueSidebar {
			s.st.OpaqueSidebar = !translucent
			s.save()
			s.a.applyTranslucency()
			if s.win != nil {
				s.win.SetVibrancy(vibrancyFor(s.st))
			}
		}
	})
}

func (s *settingsWindow) vaultsPage(c *ui.Context, pal *palette) {
	s.group(c, pal, "Recent vaults", "Removing a vault from the list leaves its folder as it is.", func() {
		if len(s.st.Recent) == 0 {
			s.row(c, pal, "No vaults yet", "Open a folder or start a new vault.", nil)
			return
		}
		for _, folder := range s.st.Recent {
			folder := folder
			s.row(c, pal, s.a.recentLabel(folder), shortPath(folder), func() {
				if button(c, "show:"+folder, revealLabel(), false, pal) {
					mygo.Shell.ShowItemInFolder(folder)
				}
				if button(c, "forget:"+folder, "Remove", false, pal) {
					s.st.Forget(folder)
					s.save()
					s.refresh()
					s.a.refreshMenu()
				}
			})
		}
	})
	s.group(c, pal, "", "", func() {
		s.row(c, pal, "Add a vault", "A vault is a folder: each page is a JSON file in it.", func() {
			if button(c, "open-vault", "Open Folder…", false, pal) {
				go s.a.openVault()
			}
			if button(c, "new-vault", "New Vault…", true, pal) {
				go s.a.newVault()
			}
		})
	})
}

func (s *settingsWindow) syncPage(c *ui.Context, pal *palette) {
	remotes := 0
	for _, folder := range s.st.Recent {
		cfg, ok := s.st.RemoteOf(folder)
		if !ok {
			continue
		}
		remotes++
		folder := folder
		s.group(c, pal, s.a.recentLabel(folder), "", func() {
			s.row(c, pal, remoteKind(cfg.Kind)+" · "+remoteWhere(cfg), lastSyncLine(s.log, folder), func() {
				if button(c, "sync:"+folder, "Sync Now", false, pal) {
					go func() {
						s.a.notebooks.syncFolder(folder)
						if s.win != nil {
							s.win.Update(func() {
								s.refresh()
								s.toast = "Synced " + s.a.recentLabel(folder)
							})
						}
					}()
				}
				if button(c, "disconnect:"+folder, "Disconnect", false, pal) {
					if err := s.a.notebooks.Disconnect(folder); err != nil {
						s.notes = "Disconnect failed: " + err.Error()
					}
					s.refresh()
				}
			})
			s.row(c, pal, "Working copy", shortPath(folder), func() {
				if button(c, "copy:"+folder, revealLabel(), false, pal) {
					mygo.Shell.ShowItemInFolder(folder)
				}
			})
			for _, item := range s.trash[folder] {
				item := item
				s.row(c, pal, item.Path, "Moved out by sync on "+item.Day, func() {
					if button(c, "restore:"+item.Trashed, "Restore", false, pal) {
						if _, err := vsync.RestoreTrash(folder, item.Trashed); err != nil {
							s.notes = "Restore failed: " + err.Error()
						}
						s.refresh()
					}
				})
			}
		})
	}
	s.group(c, pal, "Remote vaults", "Notes keeps a working copy on this computer and syncs it a few seconds after each change, every five minutes, and when a window comes forward. Passwords stay in the system's credential store.", func() {
		detail := "Keep a vault in a WebDAV folder or an SMB share."
		if remotes > 0 {
			detail = fmt.Sprintf("%d vault(s) kept in a remote folder.", remotes)
		}
		s.row(c, pal, "Connect to a remote folder", detail, func() {
			if button(c, "connect", "Connect…", true, pal) {
				go s.a.connect()
			}
		})
	})
	if len(s.log) > 0 {
		s.group(c, pal, "Recent syncs", "", func() {
			for i, e := range s.log {
				if i >= 20 {
					break
				}
				s.row(c, pal, s.a.recentLabel(e.Folder), logLine(e), nil)
			}
		})
	}
}

func (s *settingsWindow) aboutPage(c *ui.Context, pal *palette) {
	s.group(c, pal, "Notes", "", func() {
		s.row(c, pal, "Version", appVersion(), nil)
		path := s.a.settings
		s.row(c, pal, "Settings file", shortPath(path), func() {
			if button(c, "show-settings", revealLabel(), false, pal) {
				mygo.Shell.ShowItemInFolder(path)
			}
		})
		s.row(c, pal, "Sync log", shortPath(filepath.Join(filepath.Dir(path), "sync-log.json")), nil)
	})
	s.group(c, pal, "Updates", "", func() {
		s.row(c, pal, "Check for updates", updateLine(), func() {
			if button(c, "check-updates", "Check Now", false, pal) {
				updater.CheckForUpdates()
			}
		})
		if !mygo.Updater.Enabled() {
			return
		}
		s.row(c, pal, "Update source", "Automatic uses the tvv.tw mirror when the system language is Chinese, and GitHub otherwise. A failed or altered mirror download falls back to GitHub.", func() {
			for _, choice := range updateSourceChoices {
				if button(c, "update-source:"+choice, updateSourceName(choice), s.st.UpdateSource == choice, pal) && s.st.UpdateSource != choice {
					s.st.UpdateSource = choice
					s.save()
					applyUpdateSource(choice)
				}
			}
		})
		auto := updater.AutomaticChecks()
		s.row(c, pal, labelAutoUpdates, "Looks once a day and asks before installing.", func() {
			toggle(c, "auto-updates", &auto, labelAutoUpdates, pal)
		})
		if auto != updater.AutomaticChecks() {
			updater.SetAutomaticChecks(auto)
		}
	})
}

const labelAutoUpdates = "Check for updates automatically"

// updateLine says whether this build can update itself, and what the last
// check found.
func updateLine() string {
	if !mygo.Updater.Enabled() {
		return "This copy cannot update itself: it was installed by a package manager or runs from a development build."
	}
	if v := updater.AvailableVersion(); v != "" {
		return "Version " + v + " is available."
	}
	if t := updater.LastCheck(); !t.IsZero() {
		return "Last checked " + t.Local().Format("2 Jan 2006, 15:04") + "."
	}
	return "Not checked yet."
}

func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "development"
}

func revealLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Show in Finder"
	case "windows":
		return "Show in Explorer"
	}
	return "Show in Folder"
}

func remoteKind(kind string) string {
	if kind == "smb" {
		return "SMB"
	}
	return "WebDAV"
}

func remoteWhere(cfg settings.Remote) string {
	if cfg.Kind == "smb" {
		return strings.TrimRight(cfg.Address+"/"+cfg.Share+"/"+cfg.Folder, "/")
	}
	return cfg.Address
}

// lastSyncLine says when a folder last synced, and what happened.
func lastSyncLine(log []synclog.Entry, folder string) string {
	for _, e := range log {
		if e.Folder == folder {
			return "Last sync " + logLine(e)
		}
	}
	return "Not synced yet."
}

func logLine(e synclog.Entry) string {
	line := fmt.Sprintf("%s · %s · %d up, %d down, %d merged, %d conflicts",
		e.Time.Local().Format("Jan 2 15:04"), e.Status, len(e.Uploaded), len(e.Downloaded), len(e.Merged), len(e.Conflicts))
	if e.Message != "" {
		line += " · " + e.Message
	}
	return line
}
