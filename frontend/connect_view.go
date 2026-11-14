package frontend

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
	"notes/internal/storage"
)

// remoteKinds are the kinds of remote folder, in the order the form offers them.
var remoteKinds = []string{"webdav", "smb"}

// connectForm is the window that keeps a vault in a remote folder: what kind
// of folder it is, where it is and who signs in. It can test the connection
// before anything is copied. With editing set it is the sign-in of a vault
// that is connected already, whose user name or password changed.
type connectForm struct {
	a   *appActions
	win *mygo.Window

	kind                                                   int
	address, server, share, folder, user, password, domain string

	// editing is the working copy whose sign-in is changed, and remote the
	// folder it syncs with. Empty for a new connection.
	editing string
	remote  settings.Remote

	// busy is what the form is doing: "", "test" or "connect".
	busy string
	// note is what the last test found; caution says it found something to
	// think about. problem is what went wrong, and detail the error itself.
	note    string
	caution bool
	problem string
	detail  string

	// The work behind the buttons; tests replace them. async runs work off the
	// window's thread, and apply brings its result back to the window.
	check   func(ctx context.Context, cfg settings.Remote, password string) (remoteCheck, error)
	connect func(ctx context.Context, req ConnectRequest) error
	save    func(cfg settings.Remote, password string) error
	async   func(work func())
	apply   func(fn func())
	// stop ends the work that is going on, as Cancel and closing the window do.
	stop context.CancelFunc
	// sized is the height the window was last given, so that a height the
	// person chose is left alone.
	sized int
}

// The connect window's sizes: as tall as the fields of each kind need, with
// room above the buttons for two lines of what a test found.
const (
	connectWidth      = 480
	connectHeightDAV  = 506
	connectHeightSMB  = 628
	connectHeightEdit = 400
)

func newConnectForm(a *appActions) *connectForm {
	f := &connectForm{a: a}
	f.check = checkRemote
	f.connect = func(ctx context.Context, req ConnectRequest) error {
		_, err := connectRemote(ctx, a.notebooks, req)
		return err
	}
	f.save = func(cfg settings.Remote, password string) error {
		return signInAgain(a.notebooks, f.editing, cfg, password)
	}
	f.async = func(work func()) { goSafe("connect form", work) }
	f.apply = func(fn func()) {
		if f.win != nil && !f.win.IsDestroyed() {
			f.win.Update(fn)
		}
	}
	return f
}

// height is how tall the window is for what the form shows.
func (f *connectForm) height() int {
	switch {
	case f.editing != "":
		return connectHeightEdit
	case remoteKinds[f.kind] == "smb":
		return connectHeightSMB
	}
	return connectHeightDAV
}

// connect opens the window that keeps a vault in a remote folder.
func (a *appActions) connect() {
	a.openConnectForm("")
}

// signIn opens the window on the sign-in of a remote vault's working copy.
func (a *appActions) signIn(folder string) {
	a.openConnectForm(folder)
}

func (a *appActions) openConnectForm(editing string) {
	if a.connectWin != nil && !a.connectWin.IsDestroyed() {
		if a.connectView != nil && a.connectView.editing == editing {
			a.connectWin.Show()
			a.connectWin.Focus()
			return
		}
		a.connectWin.Close()
	}
	f := newConnectForm(a)
	title := tr("Connect to a Remote Folder")
	if editing != "" {
		cfg, ok := remoteOf(editing)
		if !ok {
			return
		}
		f.editing, f.remote = editing, cfg
		f.user, f.domain = cfg.User, cfg.Domain
		title = tr("Sign In Again")
	}
	f.sized = f.height()
	w := mygo.NewWindow(framed(mygo.WindowOptions{
		Title:     title,
		Width:     connectWidth,
		Height:    f.sized,
		MinWidth:  400,
		MinHeight: 360,
		Content:   ui.View(f.view),
	}))
	f.win = w
	a.connectWin = w
	a.connectView = f
	w.OnClosed(func() {
		f.halt()
		if a.connectWin == w {
			a.connectWin, a.connectView = nil, nil
		}
	})
}

// request is the form as a request, with the fields of the kind shown.
func (f *connectForm) request() ConnectRequest {
	if f.editing != "" {
		cfg := f.remote
		return ConnectRequest{Kind: cfg.Kind, Address: cfg.Address, Share: cfg.Share, Folder: cfg.Folder, User: f.user, Password: f.password, Domain: f.domain}
	}
	req := ConnectRequest{Kind: remoteKinds[f.kind], User: f.user, Password: f.password}
	if req.Kind == "smb" {
		req.Address, req.Share, req.Folder, req.Domain = f.server, f.share, f.folder, f.domain
	} else {
		req.Address = f.address
	}
	return req
}

// fail shows what went wrong: in words when the error is one that has them,
// with the error itself below.
func (f *connectForm) fail(err error) {
	f.note, f.caution = "", false
	f.problem, f.detail = remoteProblem(err)
}

// checkTimeout is how long a test of a connection waits for the server.
const checkTimeout = 45 * time.Second

// start begins the work of a button, unless the form is busy or its fields
// are not filled in. The context it returns ends when the work is cancelled.
func (f *connectForm) start(what string) (context.Context, settings.Remote, bool) {
	if f.busy != "" {
		return nil, settings.Remote{}, false
	}
	cfg, err := f.request().remote()
	if err != nil {
		f.note, f.caution, f.problem, f.detail = "", false, err.Error(), ""
		return nil, cfg, false
	}
	f.busy, f.problem, f.detail, f.note, f.caution = what, "", "", "", false
	ctx, stop := context.WithCancel(context.Background())
	f.stop = stop
	return ctx, cfg, true
}

// halt cancels the work that is going on, if any.
func (f *connectForm) halt() {
	if f.stop != nil {
		f.stop()
	}
}

// finish ends the work of a button: the form takes input again, and says
// what went wrong unless the work was cancelled. It reports whether the work
// succeeded.
func (f *connectForm) finish(ctx context.Context, err error) bool {
	f.busy = ""
	cancelled := ctx.Err() == context.Canceled
	f.halt()
	switch {
	case err == nil:
		return true
	case cancelled:
		f.note, f.caution, f.problem, f.detail = "", false, "", ""
	default:
		f.fail(err)
	}
	return false
}

// reach tests the connection within checkTimeout.
func (f *connectForm) reach(ctx context.Context, cfg settings.Remote, password string) (remoteCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return f.check(ctx, cfg, password)
}

// test reaches the folder and says what is in it. Nothing is changed.
func (f *connectForm) test() {
	ctx, cfg, ok := f.start("test")
	if !ok {
		return
	}
	password := f.password
	f.async(func() {
		found, err := f.reach(ctx, cfg, password)
		f.apply(func() {
			if f.finish(ctx, err) {
				f.note, f.caution = checkNote(found, f.editing != "")
			}
		})
	})
}

// checkNote says what a test found, and whether it is something to think
// about before connecting.
func checkNote(found remoteCheck, signedIn bool) (note string, caution bool) {
	switch {
	case signedIn:
		return tr("Signed in. The folder can be reached."), false
	case found.Vault || found.Pages > 0:
		return tr("Connected. The folder holds a vault with %d page(s).", found.Pages), false
	case found.Files == 0:
		return tr("Connected. The folder is empty: a new vault starts in it."), false
	}
	return tr("Connected, but the folder holds %d file(s) that are not a vault. All of them would be copied to this computer: an empty folder is a better place.", found.Files), true
}

// submit connects the vault, or saves the new sign-in. The folder is reached
// first, so that a wrong address or password is said before anything is
// copied or kept.
func (f *connectForm) submit() {
	ctx, cfg, ok := f.start("connect")
	if !ok {
		return
	}
	req, password, editing := f.request(), f.password, f.editing != ""
	f.async(func() {
		_, err := f.reach(ctx, cfg, password)
		switch {
		case err != nil:
		case editing:
			err = f.save(cfg, password)
		default:
			err = f.connect(ctx, req)
		}
		f.apply(func() {
			if !f.finish(ctx, err) {
				return
			}
			if !editing {
				f.a.refreshMenu()
			}
			if f.win != nil {
				f.win.Close()
			}
		})
	})
}

func (f *connectForm) view(c *ui.Context) {
	pal := paletteFor(c)
	themed(c, pal)
	backdrop(c, pal)
	c.OnShortcut(0, ui.KeyEscape, func() {
		if f.busy == "" && f.win != nil {
			f.win.Close()
		}
	})
	ui.Column(c).Fill().Padding(0, cardGap, cardGap, cardGap).Children(func() {
		ui.Row(c.Key("title")).Height(nativeBar - cardGap).Shrink(0).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Spacer(c)
			windowButtons(c, f.win, pal)
		})
		card(c, pal).Children(func() {
			// The fields scroll in a window too short for them; the buttons stay.
			ui.Scroll(c.Key("fields")).Grow(1).MinHeight(0).Padding(22, 24, 6, 24).Children(func() {
				ui.Column(c).Gap(14).Shrink(0).Children(func() {
					f.heading(c, pal)
					if f.editing == "" {
						f.kindPicker(c, pal)
						f.locationFields(c, pal)
					}
					f.accountFields(c, pal)
				})
			})
			f.outcome(c, pal)
			f.buttons(c, pal)
		})
	})
}

func (f *connectForm) heading(c *ui.Context, pal *palette) {
	ui.Column(c.Key("heading")).Gap(4).Shrink(0).Children(func() {
		if f.editing != "" {
			ui.Text(c, tr("Sign in again")).FontSize(20).FontWeight(650).TextColor(pal.text)
			ui.Text(c, remoteKindName(f.remote.Kind)+" · "+remoteWhere(f.remote)).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
			return
		}
		ui.Text(c, tr("Connect to a remote folder")).FontSize(20).FontWeight(650).TextColor(pal.text)
		ui.Text(c, tr("The vault is kept on this computer and synced with the folder you choose.")).FontSize(13).TextColor(pal.textMuted).LineHeight(1.35)
	})
}

// remoteKindName names a kind of remote folder in the language shown.
func remoteKindName(kind string) string {
	if kind == "smb" {
		return tr("SMB share")
	}
	return "WebDAV"
}

// kindPicker chooses between the kinds: each a segment, the one chosen raised.
func (f *connectForm) kindPicker(c *ui.Context, pal *palette) {
	formField(c, pal, "kind", tr("Kind"), func() {
		before := f.kind
		seg := ui.SegmentedBase(c.Key("kinds"), &f.kind, len(remoteKinds))
		seg.Track.Height(32).Padding(3).Gap(3).Radius(9).Background(pal.chip).Label(tr("Kind")).Disabled(f.busy != "").Children(func() {
			for i, kind := range remoteKinds {
				name := remoteKindName(kind)
				s := seg.Segment(i).Grow(1).Basis(0).Height(26).Radius(6.5).Center().Label(name)
				color := pal.textMuted
				switch {
				case i == f.kind:
					color = pal.text
					s.Background(pal.control)
					if pal.controlShadow {
						s.Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.13)).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.06))
					}
				case s.Hovered():
					s.Background(pal.rowHover)
				}
				s.Children(func() { ui.Text(c, name).FontSize(13).FontWeight(500).TextColor(color).SingleLine() })
			}
		})
		hint := tr("Nextcloud, Synology, Jianguoyun, Alist and other WebDAV servers.")
		if remoteKinds[f.kind] == "smb" {
			hint = tr("A shared folder on a NAS or on a Windows computer.")
		}
		ui.Text(c, hint).FontSize(12).TextColor(pal.textFaint).LineHeight(1.35)
		if f.kind != before {
			f.note, f.caution, f.problem, f.detail = "", false, "", ""
			f.fit()
		}
	})
}

// fit gives the window the height of the kind shown, unless the person gave
// it a height of their own.
func (f *connectForm) fit() {
	if f.win == nil {
		return
	}
	want := f.height()
	win, last := f.win, f.sized
	f.sized = want
	go func() {
		if w, h := win.Size(); h == last && h != want {
			win.SetSize(w, want)
		}
	}()
}

func (f *connectForm) locationFields(c *ui.Context, pal *palette) {
	if remoteKinds[f.kind] == "webdav" {
		f.input(c, pal, "address", tr("Folder address"), "https://dav.example.com/remote.php/dav/files/me/Notes", &f.address, false).AutoFocus()
		return
	}
	pair(c, "server-share",
		func() { f.input(c, pal, "server", tr("Server"), "nas.local", &f.server, false).AutoFocus() },
		func() { f.input(c, pal, "share", tr("Share"), "Notes", &f.share, false) })
	f.input(c, pal, "folder", tr("Folder in the share (optional)"), "Documents/Notes", &f.folder, false)
}

func (f *connectForm) accountFields(c *ui.Context, pal *palette) {
	pair(c, "account",
		func() { f.input(c, pal, "user", tr("User"), "", &f.user, false) },
		func() { f.input(c, pal, "password", tr("Password"), "", &f.password, true) })
	if f.editing == "" && remoteKinds[f.kind] == "smb" || f.remote.Kind == "smb" {
		f.input(c, pal, "domain", tr("Domain (optional)"), "WORKGROUP", &f.domain, false)
	}
	ui.Text(c, tr("The password is kept in the system's credential store, not in the settings file.")).FontSize(12).TextColor(pal.textFaint).LineHeight(1.35).Shrink(0)
}

// formField is a form's field: its label above, then what it holds.
func formField(c *ui.Context, pal *palette, key, label string, body func()) {
	ui.Column(c.Key(key)).Gap(5).Shrink(0).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
		body()
	})
}

// pair puts two fields side by side, each half the width.
func pair(c *ui.Context, key string, left, right func()) {
	ui.Row(c.Key(key)).Gap(12).Shrink(0).AlignItems(ui.Start).Children(func() {
		ui.Column(c.Key("left")).Grow(1).Basis(0).MinWidth(0).Children(left)
		ui.Column(c.Key("right")).Grow(1).Basis(0).MinWidth(0).Children(right)
	})
}

// input is a labeled text input; Return in it connects. It returns the input.
func (f *connectForm) input(c *ui.Context, pal *palette, key, label, placeholder string, value *string, secret bool) ui.Element {
	var in ui.Element
	formField(c, pal, key, label, func() {
		frame := ui.Row(c.Key("frame")).Cursor(ui.CursorText)
		frame.Children(func() {
			in = ui.TextInputBase(c.Key("in"), value).Grow(1).MinWidth(0).FontSize(13).
				Placeholder(placeholder).PlaceholderColor(pal.textFaint).Label(label).Disabled(f.busy != "")
			if secret {
				in.Password()
			}
			if in.Submitted() {
				f.submit()
			}
		})
		field(frame, in.Focused(), pal)
	})
	return in
}

// outcome is what the last test or the last try found, above the buttons.
func (f *connectForm) outcome(c *ui.Context, pal *palette) {
	if f.busy == "" && f.note == "" && f.problem == "" {
		return
	}
	ui.Column(c.Key("outcome")).Shrink(0).Gap(3).Padding(0, 24, 10, 24).Children(func() {
		switch {
		case f.busy == "test":
			f.working(c, pal, tr("Reaching the folder…"))
		case f.busy == "connect" && f.editing != "":
			f.working(c, pal, tr("Signing in…"))
		case f.busy == "connect":
			f.working(c, pal, tr("Connecting and copying the vault to this computer…"))
		case f.problem != "":
			ui.Text(c, f.problem).FontSize(12.5).TextColor(pal.danger).LineHeight(1.35).Role(ui.RoleAlert)
			if f.detail != "" {
				ui.Text(c, f.detail).FontSize(11.5).TextColor(pal.textFaint).LineHeight(1.35).Selectable()
			}
		default:
			color := pal.done
			if f.caution {
				color = pal.warn
			}
			ui.Text(c, f.note).FontSize(12.5).TextColor(color).LineHeight(1.35).Role(ui.RoleStatus)
		}
	})
}

func (f *connectForm) working(c *ui.Context, pal *palette, text string) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Spinner(c).Label(text)
		ui.Text(c, text).FontSize(12.5).TextColor(pal.textMuted)
	})
}

func (f *connectForm) buttons(c *ui.Context, pal *palette) {
	ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
	ui.Row(c.Key("buttons")).Shrink(0).Gap(8).AlignItems(ui.Center).Padding(12, 24, 14, 24).Children(func() {
		if button(c, "test", tr("Test Connection"), false, pal) {
			f.test()
		}
		ui.Spacer(c)
		if button(c, "cancel", tr("Cancel"), false, pal) {
			// While the form works, Cancel stops the work; else it closes the window.
			if f.busy != "" {
				f.halt()
			} else if f.win != nil {
				f.win.Close()
			}
		}
		label := tr("Connect")
		if f.editing != "" {
			label = tr("Save")
		}
		if button(c, "submit", label, true, pal) {
			f.submit()
		}
	})
}

// remoteProblem says why a remote folder could not be used, in words a
// person can act on, and gives the error itself as the detail.
func remoteProblem(err error) (problem, detail string) {
	if err == nil {
		return "", ""
	}
	if text := problemText(storage.ProblemOf(err)); text != "" {
		return text, err.Error()
	}
	return tr("The folder could not be reached."), err.Error()
}

// problemText is a problem in words, or "" for one that has none.
func problemText(p storage.Problem) string {
	switch p {
	case storage.ProblemAuth:
		return tr("The server refused the user name or the password.")
	case storage.ProblemDenied:
		return tr("This account is not allowed to use the folder.")
	case storage.ProblemNoFolder:
		return tr("The folder or the share is not on the server. Check how it is written.")
	case storage.ProblemNoHost:
		return tr("No server has that name. Check the address.")
	case storage.ProblemRefused:
		return tr("The server takes no connection there. Check the address and the port.")
	case storage.ProblemTimeout:
		return tr("The server did not answer in time.")
	case storage.ProblemBusy:
		return tr("The server is busy and asked to wait. Try again in a moment.")
	case storage.ProblemCertificate:
		return tr("The server's certificate could not be trusted.")
	case storage.ProblemNotWebDAV:
		return tr("The address answers, but not as a WebDAV folder.")
	}
	return ""
}
