package frontend

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
	"notes/internal/storage"
)

// newTestConnectForm is the connect window's form with its work done at once,
// by the functions the test gives it.
func newTestConnectForm(t *testing.T) *connectForm {
	t.Helper()
	f := newConnectForm(newTestApp(t))
	f.async = func(work func()) { work() }
	f.apply = func(fn func()) { fn() }
	f.check = func(context.Context, settings.Remote, string) (remoteCheck, error) {
		t.Fatal("the form reached a server")
		return remoteCheck{}, nil
	}
	f.connect = func(context.Context, ConnectRequest) error {
		t.Fatal("the form connected")
		return nil
	}
	return f
}

// inChinese shows the app in Chinese for one test.
func inChinese(t *testing.T) {
	t.Helper()
	before := shown.Load()
	shown.Store("zh-Hans")
	t.Cleanup(func() {
		if before == nil {
			before = "en"
		}
		shown.Store(before)
	})
}

func click(t *testing.T, tester *ui.Tester, label string) {
	t.Helper()
	if err := tester.Click(label); err != nil {
		t.Fatalf("click %q: %v; texts = %v", label, err, tester.Texts())
	}
	tester.Frame()
}

// clickIn clicks what is in a scrolling part of a window, where it is.
func clickIn(t *testing.T, tester *ui.Tester, label string) {
	t.Helper()
	box, ok := tester.Find(label)
	if !ok {
		t.Fatalf("%q is missing: %v", label, tester.Texts())
	}
	tester.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tester.Frame()
}

func TestConnectFormShowsOnlyTheFieldsOfTheKindChosen(t *testing.T) {
	f := newTestConnectForm(t)
	tester := ui.NewTester(f.view, connectWidth, connectHeightDAV)
	tester.Frame()
	for _, text := range []string{"Connect to a remote folder", "Kind", "WebDAV", "SMB share", "Folder address", "User", "Password", "Test Connection", "Cancel", "Connect"} {
		if !tester.HasText(text) {
			t.Fatalf("the WebDAV form lacks %q: %v", text, tester.Texts())
		}
	}
	for _, text := range []string{"Server", "Share", "Domain (optional)"} {
		if tester.HasText(text) {
			t.Fatalf("the WebDAV form shows %q", text)
		}
	}
	shot(t, tester, "connect-webdav")

	clickIn(t, tester, "SMB share")
	tester.SetSize(connectWidth, connectHeightSMB)
	tester.Frame()
	for _, text := range []string{"Server", "Share", "Folder in the share (optional)", "User", "Password", "Domain (optional)"} {
		if !tester.HasText(text) {
			t.Fatalf("the SMB form lacks %q: %v", text, tester.Texts())
		}
	}
	if tester.HasText("Folder address") {
		t.Fatal("the SMB form shows the WebDAV address")
	}
	shot(t, tester, "connect-smb")
}

// Every field and both buttons are in the window as it opens: nothing is
// below its bottom edge, in either language.
func TestConnectFormFitsItsWindow(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		for kind, height := range map[int]int{0: connectHeightDAV, 1: connectHeightSMB} {
			t.Run(fmt.Sprint(lang, "/", remoteKinds[kind]), func(t *testing.T) {
				if lang == "zh" {
					inChinese(t)
				}
				f := newTestConnectForm(t)
				f.kind = kind
				f.problem, f.detail = remoteProblem(errors.New("401 Unauthorized: unauthorized"))
				tester := ui.NewTester(f.view, connectWidth, height)
				tester.Frame()
				for _, text := range []string{tr("Password"), tr("Test Connection"), tr("Connect"), tr("The password is kept in the system's credential store, not in the settings file.")} {
					box, ok := tester.Find(text)
					if !ok {
						t.Fatalf("%q is missing: %v", text, tester.Texts())
					}
					if box.H <= 0 || box.Y+box.H > float32(height) {
						t.Fatalf("%q is cut off: %+v in a window %d high", text, box, height)
					}
				}
				shot(t, tester, "connect-fit-"+lang+"-"+remoteKinds[kind])
			})
		}
	}
}

func TestConnectFormIsInChinese(t *testing.T) {
	inChinese(t)
	f := newTestConnectForm(t)
	f.kind = 1
	tester := ui.NewTester(f.view, connectWidth, connectHeightSMB)
	tester.Frame()
	for _, text := range []string{"连接远程文件夹", "种类", "SMB 共享", "服务器", "共享名", "共享中的文件夹（可选）", "用户名", "密码", "域（可选）", "测试连接", "取消", "连接"} {
		if !tester.HasText(text) {
			t.Fatalf("the form lacks %q: %v", text, tester.Texts())
		}
	}
	for _, text := range []string{"Host", "SMB (Windows share)", "Connect", "Kind"} {
		if tester.HasText(text) {
			t.Fatalf("the form still shows %q in English", text)
		}
	}
	shot(t, tester, "connect-smb-zh")
	tester.SetDark(true)
	tester.Advance(300 * time.Millisecond)
	shot(t, tester, "connect-smb-zh-dark")
	// A narrow, short window keeps the buttons and scrolls the fields.
	tester.SetDark(false)
	tester.SetSize(400, 360)
	tester.Frame()
	if box, ok := tester.Find("连接"); !ok || box.Y+box.H > 360 {
		t.Fatalf("the Connect button left the window: %+v %v", box, ok)
	}
	shot(t, tester, "connect-smb-zh-narrow")
}

func TestConnectFormAsksForWhatIsMissing(t *testing.T) {
	f := newTestConnectForm(t)
	tester := ui.NewTester(f.view, connectWidth, connectHeightDAV)
	tester.Frame()
	click(t, tester, "Connect")
	if !tester.HasText("Enter the address of the folder.") {
		t.Fatalf("no word about the address: %v", tester.Texts())
	}
	f.kind, f.server = 1, "nas.local"
	tester.Frame()
	click(t, tester, "Test Connection")
	if !tester.HasText("Enter the name of the share.") {
		t.Fatalf("no word about the share: %v", tester.Texts())
	}
}

func TestConnectFormTestsTheConnectionAndSaysWhatItFound(t *testing.T) {
	f := newTestConnectForm(t)
	f.address, f.user, f.password = "dav.example.com/Notes", " ann ", "secret"
	var asked settings.Remote
	var with string
	found := remoteCheck{Files: 9, Pages: 7, Vault: true}
	f.check = func(_ context.Context, cfg settings.Remote, password string) (remoteCheck, error) {
		asked, with = cfg, password
		return found, nil
	}
	tester := ui.NewTester(f.view, connectWidth, connectHeightDAV)
	tester.Frame()
	click(t, tester, "Test Connection")
	want := settings.Remote{Kind: "webdav", Address: "https://dav.example.com/Notes", User: "ann"}
	if asked != want || with != "secret" {
		t.Fatalf("tested %+v with %q, want %+v", asked, with, want)
	}
	if !tester.HasText("Connected. The folder holds a vault with 7 page(s).") {
		t.Fatalf("texts = %v", tester.Texts())
	}
	shot(t, tester, "connect-tested")

	found = remoteCheck{}
	click(t, tester, "Test Connection")
	if !tester.HasText("Connected. The folder is empty: a new vault starts in it.") {
		t.Fatalf("texts = %v", tester.Texts())
	}
	found = remoteCheck{Files: 31}
	click(t, tester, "Test Connection")
	if !f.caution || tester.HasText("Connected. The folder is empty: a new vault starts in it.") {
		t.Fatalf("a folder of other files should be a caution: %v", tester.Texts())
	}
	shot(t, tester, "connect-caution")
}

func TestConnectFormSaysWhyTheServerRefused(t *testing.T) {
	f := newTestConnectForm(t)
	f.address = "https://dav.example.com/Notes"
	f.check = func(context.Context, settings.Remote, string) (remoteCheck, error) {
		return remoteCheck{}, fmt.Errorf("sync: listing the remote folder: %w", errors.New("401 Unauthorized"))
	}
	tester := ui.NewTester(f.view, connectWidth, connectHeightDAV)
	tester.Frame()
	click(t, tester, "Test Connection")
	if !tester.HasText("The server refused the user name or the password.") || !tester.HasText("sync: listing the remote folder: 401 Unauthorized") {
		t.Fatalf("texts = %v", tester.Texts())
	}
	shot(t, tester, "connect-refused")
	// A folder that is reached and then fails while it is copied says so too.
	f.check = func(context.Context, settings.Remote, string) (remoteCheck, error) { return remoteCheck{}, nil }
	f.connect = func(context.Context, ConnectRequest) error {
		return fmt.Errorf("%w: 503 Service Unavailable", storage.ErrBusy)
	}
	click(t, tester, "Connect")
	if !tester.HasText("The server is busy and asked to wait. Try again in a moment.") || f.busy != "" {
		t.Fatalf("texts = %v, busy = %q", tester.Texts(), f.busy)
	}
}

func TestConnectFormConnectsWithTheFieldsOfItsKind(t *testing.T) {
	f := newTestConnectForm(t)
	f.kind = 1
	f.address = "https://left.over/from/webdav"
	f.server, f.share, f.folder, f.user, f.password, f.domain = "nas.local", "Notes", "Team", "ann", "secret", "HOME"
	var got ConnectRequest
	f.check = func(context.Context, settings.Remote, string) (remoteCheck, error) { return remoteCheck{}, nil }
	f.connect = func(_ context.Context, req ConnectRequest) error {
		got = req
		return errors.New("stop here")
	}
	tester := ui.NewTester(f.view, connectWidth, connectHeightSMB)
	tester.Frame()
	click(t, tester, "Connect")
	want := ConnectRequest{Kind: "smb", Address: "nas.local", Share: "Notes", Folder: "Team", User: "ann", Password: "secret", Domain: "HOME"}
	if got != want {
		t.Fatalf("connected with %+v, want %+v", got, want)
	}
}

// A wrong address or password is said before a working copy is made.
func TestConnectReachesTheFolderBeforeItCopiesAnything(t *testing.T) {
	f := newTestConnectForm(t)
	f.address = "https://dav.example.com/Notes"
	f.check = func(context.Context, settings.Remote, string) (remoteCheck, error) {
		return remoteCheck{}, errors.New("404 Not Found")
	}
	tester := ui.NewTester(f.view, connectWidth, connectHeightDAV)
	tester.Frame()
	click(t, tester, "Connect") // f.connect fails the test if it is reached
	if !tester.HasText("The folder or the share is not on the server. Check how it is written.") {
		t.Fatalf("texts = %v", tester.Texts())
	}
}

func TestCancelStopsATestThatTheServerDoesNotAnswer(t *testing.T) {
	f := newTestConnectForm(t)
	f.address = "https://dav.example.com/Notes"
	started, finished := make(chan struct{}), make(chan struct{})
	f.async = func(work func()) { go work() }
	f.apply = func(fn func()) { fn(); close(finished) }
	f.check = func(ctx context.Context, _ settings.Remote, _ string) (remoteCheck, error) {
		close(started)
		<-ctx.Done() // a server that never answers
		return remoteCheck{}, ctx.Err()
	}
	f.test()
	<-started
	if f.busy != "test" {
		t.Fatalf("busy = %q while the test runs", f.busy)
	}
	f.halt()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the test went on after Cancel")
	}
	if f.busy != "" || f.problem != "" || f.note != "" {
		t.Fatalf("after Cancel: busy %q, problem %q, note %q", f.busy, f.problem, f.note)
	}
}

func TestSignInAgainKeepsTheFolderAndChangesTheAccount(t *testing.T) {
	f := newTestConnectForm(t)
	f.editing = "/vaults/abc"
	f.remote = settings.Remote{Kind: "smb", Address: "nas.local", Share: "Notes", Folder: "Team", User: "ann", Domain: "HOME"}
	f.user, f.domain, f.password = "bob", "HOME", "new"
	f.check = func(context.Context, settings.Remote, string) (remoteCheck, error) { return remoteCheck{}, nil }
	var saved settings.Remote
	var with string
	f.save = func(cfg settings.Remote, password string) error {
		saved, with = cfg, password
		return nil
	}
	tester := ui.NewTester(f.view, connectWidth, connectHeightEdit)
	tester.Frame()
	if !tester.HasText("Sign in again") || !tester.HasText("SMB share · nas.local/Notes/Team") || tester.HasText("Server") || tester.HasText("Kind") {
		t.Fatalf("texts = %v", tester.Texts())
	}
	shot(t, tester, "connect-sign-in")
	click(t, tester, "Save")
	want := settings.Remote{Kind: "smb", Address: "nas.local", Share: "Notes", Folder: "Team", User: "bob", Domain: "HOME"}
	if saved != want || with != "new" {
		t.Fatalf("saved %+v with %q, want %+v", saved, with, want)
	}
}

func TestAddressesAreReadAsPeopleWriteThem(t *testing.T) {
	for _, tc := range []struct {
		req  ConnectRequest
		want settings.Remote
	}{
		{ConnectRequest{Kind: "webdav", Address: " dav.example.com/Notes ", Share: "x", Domain: "y"}, settings.Remote{Kind: "webdav", Address: "https://dav.example.com/Notes"}},
		{ConnectRequest{Kind: "webdav", Address: "http://192.168.1.5:5005/Notes"}, settings.Remote{Kind: "webdav", Address: "http://192.168.1.5:5005/Notes"}},
		{ConnectRequest{Kind: "smb", Address: "nas.local", Share: "/Notes/", Folder: `Team\2026\`}, settings.Remote{Kind: "smb", Address: "nas.local", Share: "Notes", Folder: "Team/2026"}},
		{ConnectRequest{Kind: "smb", Address: "smb://nas.local/Notes/Team/2026"}, settings.Remote{Kind: "smb", Address: "nas.local", Share: "Notes", Folder: "Team/2026"}},
		{ConnectRequest{Kind: "smb", Address: `\\NAS\Notes`, Folder: "Team"}, settings.Remote{Kind: "smb", Address: "NAS", Share: "Notes", Folder: "Team"}},
		{ConnectRequest{Kind: "smb", Address: "10.0.0.2:4450", Share: "Notes"}, settings.Remote{Kind: "smb", Address: "10.0.0.2:4450", Share: "Notes"}},
	} {
		got, err := tc.req.remote()
		if err != nil || got != tc.want {
			t.Errorf("%+v reads as %+v (%v), want %+v", tc.req, got, err, tc.want)
		}
	}
	for _, req := range []ConnectRequest{{Kind: "webdav"}, {Kind: "smb", Share: "Notes"}, {Kind: "smb", Address: "nas.local"}, {Kind: "ftp", Address: "x"}} {
		if _, err := req.remote(); err == nil {
			t.Errorf("%+v should be refused", req)
		}
	}
}

func TestATestOfAConnectionCountsWhatTheFolderHolds(t *testing.T) {
	dir := t.TempDir()
	remote := storage.Dir{Root: dir}
	ctx := context.Background()
	for _, p := range []string{".notes/vault.json", "pages/p_1.json", "pages/p_2.json", "attachments/p_1/a.png", ".trash/2026-11-10/pages/p_9.json"} {
		if _, err := remote.Put(ctx, p, []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := remote.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := countRemote(entries)
	if want := (remoteCheck{Files: 4, Pages: 2, Vault: true}); got != want {
		t.Fatalf("found %+v, want %+v", got, want)
	}
}
