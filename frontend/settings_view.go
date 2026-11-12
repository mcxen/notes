// Settings window adapted from github.com/egoist/kero (MIT), through GodGo's
// settings_view.go: a sidebar of pages with a search field, and the page on a
// raised card, its settings in grouped rows.
package frontend

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"notes/internal/mcpsvc"
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
	{"integrations", "Integrations", plugGlyph},
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

	// The MCP port and public address as typed, until Return saves them.
	mcpPort   string
	mcpPublic string
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
		Title:     tr("Settings"),
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
		s.mcpPort = fmt.Sprint(st.MCP.PortOrDefault())
		s.mcpPublic = st.MCP.PublicURL
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
		s.notes = tr("The settings could not be saved: %v", err)
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
				ui.Text(c, tr("No settings match")).FontSize(13).TextColor(pal.textFaint).Padding(8, 10)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Padding(cardGap, cardGap, cardGap, 0).Children(func() {
			card(c, pal).Children(func() {
				page := s.current()
				ui.Row(c.Key("header")).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 10, 0, 24).DragWindow().Children(func() {
					ui.Text(c, tr(page.title)).FontSize(15).FontWeight(650).TextColor(pal.text)
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
						case "integrations":
							s.integrationsPage(c, pal)
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
	item := ui.Row(c.Key(p.id)).Height(32).Padding(0, 10).Gap(10).AlignItems(ui.Center).Radius(8).Label(tr(p.title))
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
		ui.Text(c, tr(p.title)).FontSize(13.5).TextColor(pal.text).SingleLine()
	})
}

func (s *settingsWindow) searchField(c *ui.Context, pal *palette) {
	var in ui.Element
	frame := ui.Row(c.Key("search")).Cursor(ui.CursorText).Margin(0, 0, 10, 0)
	frame.Children(func() {
		ui.Icon(c, searchGlyph).Size(13, 13).TextColor(pal.textFaint).Shrink(0)
		in = ui.TextInputBase(c.Key("in"), &s.query).Bind(&s.searchRef).Grow(1).MinWidth(0).FontSize(13).
			Placeholder(tr("Search settings")).Label(tr("Search settings"))
		in.OnShortcut(0, ui.KeyEscape, func() { s.query = "" })
		if s.query != "" {
			x := ui.ButtonBase(c.Key("clear")).Size(16, 16).Radius(8).Center().Shrink(0).Label(tr("Clear search")).
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
	{"general", labelPickerAtLaunch, "startup launch open picker last vault 启动 选择器 笔记库"},
	{"appearance", labelTranslucent, "translucent vibrancy material transparent glass 半透明 侧栏 材质"},
	{"appearance", labelOutline, "outline headings table of contents toc 大纲 目录 标题"},
	{"vaults", "Recent vaults", "vault folder recent remove show finder explorer"},
	{"vaults", "Add a vault", "new open folder"},
	{"sync", "Remote vaults", "sync webdav smb remote cloud disconnect"},
	{"sync", "Recent syncs", "log history conflicts trash restore"},
	{"integrations", "MCP server", "mcp ai assistant claude cursor key token port frp proxy api integrations"},
	{"integrations", "Vaults for assistants", "mcp vault allow assistants read only"},
	{"integrations", "Connect an assistant", "prompt copy claude code command stdio config"},
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
		if matches(tr(p.title)+" "+p.title, terms) {
			found[p.id] = []string{}
		}
	}
	for _, e := range searchEntries {
		label := tr(e.label)
		if matches(label+" "+e.label+" "+e.words, terms) {
			found[e.page] = append(found[e.page], label)
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
	labelAutoUpdates    = "Check for updates automatically"
	labelOutline        = "Show the page outline"
)

func (s *settingsWindow) generalPage(c *ui.Context, pal *palette) {
	s.group(c, pal, tr("Language"), "", func() {
		s.row(c, pal, tr("Language"), tr("Automatic follows the system's language. The pages and the menus change at once."), func() {
			for _, choice := range languageChoices {
				if button(c, "language:"+choice, languageName(choice), s.st.Language == choice, pal) && s.st.Language != choice {
					s.st.Language = choice
					s.save()
					applyLanguage(choice)
					s.a.refreshMenu()
				}
			}
		})
	})
	s.group(c, pal, tr("Startup"), "", func() {
		picker := s.st.PickerAtLaunch
		s.row(c, pal, tr(labelPickerAtLaunch), tr("When it is off, Notes reopens the vault you used last."), func() {
			toggle(c, "picker-at-launch", &picker, tr(labelPickerAtLaunch), pal)
		})
		if picker != s.st.PickerAtLaunch {
			s.st.PickerAtLaunch = picker
			s.save()
		}
	})
	s.group(c, pal, tr("Shortcuts"), "", func() {
		mod := "⌘"
		if runtime.GOOS != "darwin" {
			mod = "Ctrl+"
		}
		for _, k := range [][2]string{
			{tr("Open a vault"), mod + "O"},
			{tr("Vault picker"), mod + "⇧O"},
			{tr("Sync now"), mod + "⇧S"},
			{tr("Toggle the sidebar"), mod + "\\"},
			{tr("Settings"), mod + ","},
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
		s.group(c, pal, tr("Window"), "", func() {
			s.row(c, pal, tr(labelTranslucent), tr("Available on macOS."), nil)
		})
	} else {
		s.group(c, pal, tr("Window"), tr("Light and dark follow the system, or the theme you pick in a vault's sidebar menu."), func() {
			translucent := !s.st.OpaqueSidebar
			s.row(c, pal, tr(labelTranslucent), tr("The sidebar shows the desktop through it, as Mac apps do."), func() {
				toggle(c, "translucent", &translucent, tr(labelTranslucent), pal)
			})
			if translucent == s.st.OpaqueSidebar {
				s.st.OpaqueSidebar = !translucent
				s.save()
				s.a.applyLook()
				if s.win != nil {
					s.win.SetVibrancy(vibrancyFor(s.st))
				}
			}
		})
	}
	s.group(c, pal, tr("Page"), "", func() {
		outline := !s.st.HideOutline
		s.row(c, pal, tr(labelOutline), tr("Short lines at the page's right edge, one for each heading: point at them to see the headings, press one to go there. Pages with fewer than two headings, and narrow windows, have none."), func() {
			toggle(c, "outline", &outline, tr(labelOutline), pal)
		})
		if outline == s.st.HideOutline {
			s.st.HideOutline = !outline
			s.save()
			s.a.applyLook()
		}
	})
}

func (s *settingsWindow) vaultsPage(c *ui.Context, pal *palette) {
	s.group(c, pal, tr("Recent vaults"), tr("Removing a vault from the list leaves its folder as it is."), func() {
		if len(s.st.Recent) == 0 {
			s.row(c, pal, tr("No vaults yet"), tr("Open a folder or start a new vault."), nil)
			return
		}
		for _, folder := range s.st.Recent {
			folder := folder
			s.row(c, pal, s.a.recentLabel(folder), shortPath(folder), func() {
				if button(c, "show:"+folder, revealLabel(), false, pal) {
					mygo.Shell.ShowItemInFolder(folder)
				}
				if button(c, "forget:"+folder, tr("Remove"), false, pal) {
					s.st.Forget(folder)
					s.save()
					s.refresh()
					s.a.refreshMenu()
				}
			})
		}
	})
	s.group(c, pal, "", "", func() {
		s.row(c, pal, tr("Add a vault"), tr("A vault is a folder: each page is a JSON file in it."), func() {
			if button(c, "open-vault", tr("Open Folder…"), false, pal) {
				go s.a.openVault()
			}
			if button(c, "new-vault", tr("New Vault…"), true, pal) {
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
				if button(c, "sync:"+folder, tr("Sync Now"), false, pal) {
					go func() {
						s.a.notebooks.syncFolder(folder)
						if s.win != nil {
							s.win.Update(func() {
								s.refresh()
								s.toast = tr("Synced %s", s.a.recentLabel(folder))
							})
						}
					}()
				}
				if button(c, "disconnect:"+folder, tr("Disconnect"), false, pal) {
					if err := s.a.notebooks.Disconnect(folder); err != nil {
						s.notes = tr("Disconnect failed: %v", err)
					}
					s.refresh()
				}
			})
			s.row(c, pal, tr("Working copy"), shortPath(folder), func() {
				if button(c, "copy:"+folder, revealLabel(), false, pal) {
					mygo.Shell.ShowItemInFolder(folder)
				}
			})
			for _, item := range s.trash[folder] {
				item := item
				s.row(c, pal, item.Path, tr("Moved out by sync on %s", item.Day), func() {
					if button(c, "restore:"+item.Trashed, tr("Restore"), false, pal) {
						if _, err := vsync.RestoreTrash(folder, item.Trashed); err != nil {
							s.notes = tr("Restore failed: %v", err)
						}
						s.refresh()
					}
				})
			}
		})
	}
	s.group(c, pal, tr("Remote vaults"), tr("Notes keeps a working copy on this computer and syncs it a few seconds after each change, every five minutes, and when a window comes forward. Passwords stay in the system's credential store."), func() {
		detail := tr("Keep a vault in a WebDAV folder or an SMB share.")
		if remotes > 0 {
			detail = tr("%d vault(s) kept in a remote folder.", remotes)
		}
		s.row(c, pal, tr("Connect to a remote folder"), detail, func() {
			if button(c, "connect", tr("Connect…"), true, pal) {
				go s.a.connect()
			}
		})
	})
	if len(s.log) > 0 {
		s.group(c, pal, tr("Recent syncs"), "", func() {
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
	s.group(c, pal, tr("Notes"), "", func() {
		s.row(c, pal, tr("Version"), appVersion(), nil)
		path := s.a.settings
		s.row(c, pal, tr("Settings file"), shortPath(path), func() {
			if button(c, "show-settings", revealLabel(), false, pal) {
				mygo.Shell.ShowItemInFolder(path)
			}
		})
		s.row(c, pal, tr("Sync log"), shortPath(filepath.Join(filepath.Dir(path), "sync-log.json")), nil)
	})
	s.group(c, pal, tr("Updates"), "", func() {
		s.row(c, pal, tr("Check for updates"), updateLine(), func() {
			if button(c, "check-updates", tr("Check Now"), false, pal) {
				updater.CheckForUpdates()
			}
		})
		if !mygo.Updater.Enabled() {
			return
		}
		s.row(c, pal, tr("Update source"), tr("Automatic uses the tvv.tw mirror when the system language is Chinese, and GitHub otherwise. A failed or altered mirror download falls back to GitHub."), func() {
			for _, choice := range updateSourceChoices {
				if button(c, "update-source:"+choice, updateSourceName(choice), s.st.UpdateSource == choice, pal) && s.st.UpdateSource != choice {
					s.st.UpdateSource = choice
					s.save()
					applyUpdateSource(choice)
				}
			}
		})
		auto := updater.AutomaticChecks()
		s.row(c, pal, tr(labelAutoUpdates), tr("Looks once a day and asks before installing."), func() {
			toggle(c, "auto-updates", &auto, tr(labelAutoUpdates), pal)
		})
		if auto != updater.AutomaticChecks() {
			updater.SetAutomaticChecks(auto)
		}
	})
}

// updateLine says whether this build can update itself, and what the last
// check found.
func updateLine() string {
	if !mygo.Updater.Enabled() {
		return tr("This copy cannot update itself: it was installed by a package manager or runs from a development build.")
	}
	if v := updater.AvailableVersion(); v != "" {
		return tr("Version %s is available.", v)
	}
	if t := updater.LastCheck(); !t.IsZero() {
		return tr("Last checked %s.", t.Local().Format(dateTimeLayout()))
	}
	return tr("Not checked yet.")
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
		return tr("Show in Finder")
	case "windows":
		return tr("Show in Explorer")
	}
	return tr("Show in Folder")
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
			return tr("Last sync %s", logLine(e))
		}
	}
	return tr("Not synced yet.")
}

func logLine(e synclog.Entry) string {
	line := tr("%s · %s · %d up, %d down, %d merged, %d conflicts",
		e.Time.Local().Format(shortTimeLayout()), e.Status, len(e.Uploaded), len(e.Downloaded), len(e.Merged), len(e.Conflicts))
	if e.Message != "" {
		line += " · " + e.Message
	}
	return line
}

// integrationsPage is Settings › Integrations: the MCP server that lets AI
// assistants read and edit the vaults the user allows.
func (s *settingsWindow) integrationsPage(c *ui.Context, pal *palette) {
	s.group(c, pal, tr("MCP server"), tr("Lets AI assistants read and edit the vaults you allow. Off by default; every request needs the key."), func() {
		on := s.st.MCP.Enabled
		s.row(c, pal, tr("Turn on the MCP server"), s.a.mcp.status(s.st), func() {
			toggle(c, "mcp-on", &on, tr("Turn on the MCP server"), pal)
		})
		if on != s.st.MCP.Enabled {
			s.st.MCP.Enabled = on
			if on && s.st.MCP.Key == "" {
				s.st.MCP.Key = mcpsvc.NewKey()
			}
			s.save()
			s.a.applyMCP()
			s.refresh()
		}
		s.row(c, pal, tr("Port"), tr("Press Return to apply. Default 8999."), func() {
			var in ui.Element
			frame := ui.Row(c.Key("mcp-port-frame")).Width(96)
			frame.Children(func() {
				in = ui.TextInputBase(c.Key("mcp-port"), &s.mcpPort).Grow(1).MinWidth(0).FontSize(13)
			})
			field(frame, in.Focused(), pal)
			if in.Submitted() {
				port, err := strconv.Atoi(strings.TrimSpace(s.mcpPort))
				if err != nil || port < 1 || port > 65535 {
					s.notes = tr("The port must be a number from 1 to 65535.")
					return
				}
				s.st.MCP.Port = port
				if port == settings.DefaultMCPPort {
					s.st.MCP.Port = 0
				}
				s.save()
				s.a.applyMCP()
				s.refresh()
			}
		})
		local := s.st.MCP.LocalOnly
		s.row(c, pal, tr("Only this computer"), tr("Off: the server listens on every network interface, so other computers and a proxy such as frp can reach it. The key is still required."), func() {
			toggle(c, "mcp-local", &local, tr("Only this computer"), pal)
		})
		if local != s.st.MCP.LocalOnly {
			s.st.MCP.LocalOnly = local
			s.save()
			s.a.applyMCP()
			s.refresh()
		}
		readOnly := s.st.MCP.ReadOnly
		s.row(c, pal, tr("Read only"), tr("Assistants can read and search, but not change anything."), func() {
			toggle(c, "mcp-readonly", &readOnly, tr("Read only"), pal)
		})
		if readOnly != s.st.MCP.ReadOnly {
			s.st.MCP.ReadOnly = readOnly
			s.save()
			s.refresh()
		}
		s.row(c, pal, tr("Public address"), tr("Optional, e.g. https://notes.example.com/mcp, for a proxy. Shown in the prompt. Press Return to save."), func() {
			var in ui.Element
			frame := ui.Row(c.Key("mcp-public-frame")).Width(240)
			frame.Children(func() {
				in = ui.TextInputBase(c.Key("mcp-public"), &s.mcpPublic).Grow(1).MinWidth(0).FontSize(13)
			})
			field(frame, in.Focused(), pal)
			if in.Submitted() {
				s.st.MCP.PublicURL = strings.TrimSpace(s.mcpPublic)
				s.save()
				s.refresh()
			}
		})
		s.row(c, pal, tr("Key"), maskedKey(s.st.MCP.Key), func() {
			if button(c, "mcp-copy-key", tr("Copy key"), false, pal) && s.st.MCP.Key != "" {
				mygo.Clipboard.WriteText(s.st.MCP.Key)
				s.toast = tr("Copied.")
			}
			if button(c, "mcp-new-key", tr("Make a new key"), false, pal) {
				s.st.MCP.Key = mcpsvc.NewKey()
				s.save()
				s.a.applyMCP()
				s.refresh()
				s.toast = tr("A new key is made. The old one no longer works.")
			}
		})
	})
	s.group(c, pal, tr("Vaults for assistants"), tr("The first vault is the default; a call can name another one."), func() {
		if len(s.st.Recent) == 0 {
			s.row(c, pal, tr("No vaults yet"), tr("Open a folder or start a new vault."), nil)
			return
		}
		for _, folder := range s.st.Recent {
			folder := folder
			allowed := slices.Contains(s.st.MCP.Vaults, folder)
			detail := shortPath(folder)
			if allowed && len(s.st.MCP.Vaults) > 0 && s.st.MCP.Vaults[0] == folder {
				detail += " · " + tr("default")
			}
			s.row(c, pal, s.a.recentLabel(folder), detail, func() {
				toggle(c, "mcp-vault:"+folder, &allowed, s.a.recentLabel(folder), pal)
			})
			if allowed != slices.Contains(s.st.MCP.Vaults, folder) {
				if allowed {
					s.st.MCP.Vaults = append(s.st.MCP.Vaults, folder)
				} else {
					s.st.MCP.Vaults = slices.DeleteFunc(s.st.MCP.Vaults, func(f string) bool { return f == folder })
				}
				s.save()
				s.refresh()
			}
		}
	})
	s.group(c, pal, tr("Connect an assistant"), tr("The prompt has the address, the key, the vaults and the tools: paste it into the assistant's instructions."), func() {
		s.row(c, pal, tr("Copy the prompt"), tr("Everything an assistant needs, in one text."), func() {
			if button(c, "mcp-copy-prompt", tr("Copy the prompt"), true, pal) {
				mygo.Clipboard.WriteText(mcpPrompt(s.st))
				s.toast = tr("The prompt is copied.")
			}
		})
		s.row(c, pal, tr("Copy the Claude Code command"), tr("Adds the server to Claude Code with the key."), func() {
			if button(c, "mcp-copy-claude", tr("Copy"), false, pal) {
				mygo.Clipboard.WriteText(claudeCodeCommand(s.st))
				s.toast = tr("Copied.")
			}
		})
		s.row(c, pal, tr("Copy the stdio entry"), tr("For a client that starts a program instead of an address."), func() {
			if button(c, "mcp-copy-stdio", tr("Copy"), false, pal) {
				mygo.Clipboard.WriteText(stdioConfig())
				s.toast = tr("Copied.")
			}
		})
	})
}
