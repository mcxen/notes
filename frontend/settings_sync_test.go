package frontend

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
	vsync "notes/internal/sync"
	"notes/internal/synclog"
)

// newSyncPage is the settings window on its Sync page, with one remote vault
// and the log given.
func newSyncPage(t *testing.T, log ...synclog.Entry) (*settingsWindow, string) {
	t.Helper()
	a := newTestApp(t)
	s := &settingsWindow{a: a, page: "sync"}
	s.refresh()
	folder := s.st.Recent[0]
	s.st.SetRemote(folder, settings.Remote{Kind: "webdav", Address: "https://dav.example.com/Notes", User: "ann"})
	for i := range log {
		log[i].Folder = folder
	}
	s.log = log
	return s, folder
}

// hasTextWith reports whether some text of the window contains part.
func hasTextWith(tester *ui.Tester, part string) bool {
	for _, text := range tester.Texts() {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}

func TestSyncPageSaysWhenAVaultLastSyncedAndWhatMoved(t *testing.T) {
	at := time.Date(2026, 11, 14, 10, 32, 0, 0, time.Local)
	s, _ := newSyncPage(t, synclog.Entry{Time: at, Status: "saved", Uploaded: []string{"a", "b"}, Downloaded: []string{"c"}})
	tester := ui.NewTester(s.view, 820, 580)
	tester.Frame()
	for _, text := range []string{"WebDAV · https://dav.example.com/Notes", "Synced Nov 14 10:32 · 2 uploaded, 1 downloaded", "Sync Now", "Signed in as", "ann", "Sign In Again…", "Stop syncing", "Disconnect…"} {
		if !tester.HasText(text) {
			t.Fatalf("the Sync page lacks %q: %v", text, tester.Texts())
		}
	}
	shot(t, tester, "settings-sync-synced")
}

func TestSyncPageSaysWhyAVaultCouldNotSync(t *testing.T) {
	at := time.Date(2026, 11, 14, 10, 32, 0, 0, time.Local)
	s, _ := newSyncPage(t, synclog.Entry{Time: at, Status: "offline", Problem: "auth", Message: "sync: listing the remote folder: 401 Unauthorized"})
	tester := ui.NewTester(s.view, 820, 580)
	tester.Frame()
	if !tester.HasText("Could not sync Nov 14 10:32 · The server refused the user name or the password.") {
		t.Fatalf("texts = %v", tester.Texts())
	}
	if status := s.syncStatus(s.st.Recent[0]); !status.failed || !status.auth {
		t.Fatalf("status = %+v", status)
	}
	shot(t, tester, "settings-sync-refused")

	// A failure the log has no name for is shown as it was written.
	s.log[0].Problem = ""
	tester.Frame()
	if !hasTextWith(tester, "sync: listing the remote folder: 401 Unauthorized") {
		t.Fatalf("texts = %v", tester.Texts())
	}
}

func TestSyncPageShowsASyncThatIsRunning(t *testing.T) {
	s, folder := newSyncPage(t, synclog.Entry{Time: time.Now(), Status: "saved"})
	changed := 0
	s.a.notebooks.watchSync(func() { changed++ })
	s.a.notebooks.noteSync(folder, SyncState{Status: "saving"})
	tester := ui.NewTester(s.view, 820, 580)
	tester.Frame()
	if !tester.HasText("Syncing…") || tester.HasText("Sync Now") || changed != 1 {
		t.Fatalf("changed = %d, texts = %v", changed, tester.Texts())
	}
	s.a.notebooks.noteSync(folder, SyncState{Status: "saved"})
	tester.Frame()
	if !tester.HasText("Sync Now") || !hasTextWith(tester, "nothing to change") {
		t.Fatalf("texts = %v", tester.Texts())
	}
}

func TestSyncPageIsInChinese(t *testing.T) {
	inChinese(t)
	at := time.Date(2026, 11, 14, 10, 32, 0, 0, time.Local)
	s, _ := newSyncPage(t,
		synclog.Entry{Time: at, Status: "refused", Uploaded: []string{"a"}, Failed: []string{"pages/p_1.json: 507 Insufficient Storage"}},
		synclog.Entry{Time: at.Add(-time.Hour), Status: "offline", Problem: "timeout"},
		synclog.Entry{Time: at.Add(-2 * time.Hour), Status: "saved", Merged: []string{"a"}, Conflicts: []string{"a"}, Trashed: []string{"b"}},
	)
	tester := ui.NewTester(s.view, 820, 640)
	tester.Frame()
	for _, text := range []string{"11月14日 10:32 已同步，但有文件未完成 · pages/p_1.json: 507 Insufficient Storage", "登录账户", "重新登录…", "停止同步", "断开连接…", "立即同步"} {
		if !tester.HasText(text) {
			t.Fatalf("the Sync page lacks %q: %v", text, tester.Texts())
		}
	}
	for _, part := range []string{"同步失败 · 服务器没有及时响应。", "合并 1，冲突 1，移入回收站 1"} {
		if !hasTextWith(tester, part) {
			t.Fatalf("the Sync page lacks %q: %v", part, tester.Texts())
		}
	}
	shot(t, tester, "settings-sync-zh")
}

func TestDisconnectAsksFirst(t *testing.T) {
	s, folder := newSyncPage(t)
	asked := ""
	before := confirmDisconnect
	t.Cleanup(func() { confirmDisconnect = before })
	confirmDisconnect = func(_ *mygo.Window, name string) bool {
		asked = name
		return false
	}
	s.disconnect(folder)
	if asked == "" {
		t.Fatal("the vault was disconnected without asking")
	}
	if _, ok := s.st.RemoteOf(folder); !ok {
		t.Fatal("a disconnect that was not confirmed forgot the remote folder")
	}
}

func TestSyncPageOffersAWayOutOfHeldDeletions(t *testing.T) {
	at := time.Date(2026, 11, 14, 10, 32, 0, 0, time.Local)
	s, _ := newSyncPage(t, synclog.Entry{Time: at, Status: "held", HeldLocal: []string{"pages/a.json", "pages/b.json", "pages/c.json"}})
	tester := ui.NewTester(s.view, 820, 640)
	tester.Frame()
	for _, text := range []string{"3 file(s) wait for you", "Copy Them Back to the Remote Folder", "Remove Them Here Too"} {
		if !tester.HasText(text) {
			t.Fatalf("the Sync page lacks %q: %v", text, tester.Texts())
		}
	}
	shot(t, tester, "settings-sync-held")

	s.log[0] = synclog.Entry{Folder: s.st.Recent[0], Time: at, Status: "held", HeldRemote: []string{"pages/a.json"}}
	tester.Frame()
	if !tester.HasText("Bring Them Back Here") || !tester.HasText("Remove Them in the Remote Folder Too") {
		t.Fatalf("texts = %v", tester.Texts())
	}
}

func TestSyncOutcomeSaysWhatWaitsForThePerson(t *testing.T) {
	state, status := syncOutcome(vsync.Report{HeldLocal: []string{"a", "b"}})
	if status != "held" || state.Held != 2 || state.Status != "refused" || state.Message == "" {
		t.Fatalf("held: %+v %q", state, status)
	}
	if _, status := syncOutcome(vsync.Report{Failed: []string{"a: boom"}, HeldLocal: []string{"b"}}); status != "refused" {
		t.Fatalf("a failure comes first: %q", status)
	}
	if state, status := syncOutcome(vsync.Report{Deferred: []string{"a"}}); status != "saved" || state.Status != "saved" {
		t.Fatalf("a file left for the next sync is not a failure: %+v %q", state, status)
	}
}
