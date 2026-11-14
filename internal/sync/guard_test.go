package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notes/internal/storage"
)

// meanwhile is a remote that lets something happen just before one of its
// calls: another device syncing, or the person typing, while a sync runs.
type meanwhile struct {
	storage.Remote
	beforeGet, beforePut func(p string)
}

func (m *meanwhile) Get(ctx context.Context, p string) ([]byte, error) {
	if m.beforeGet != nil {
		m.beforeGet(p)
	}
	return m.Remote.Get(ctx, p)
}

func (m *meanwhile) Put(ctx context.Context, p string, data []byte) (storage.Entry, error) {
	if m.beforePut != nil {
		m.beforePut(p)
	}
	return m.Remote.Put(ctx, p, data)
}

// once runs fn the first time it is called.
func once(fn func()) func(string) {
	done := false
	return func(string) {
		if !done {
			done = true
			fn()
		}
	}
}

func emptyFolder(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnEditMadeDuringADownloadIsNotWrittenOver(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	b := newDevice(t, "B", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	a.sync()
	b.sync()
	b.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "from B"}))
	b.sync()

	// While A fetches B's version, the person types on A and the vault saves.
	typed := page(map[string]string{"b1": "one", "b3": "typed on A"})
	a.syncer.Remote = &meanwhile{Remote: remote, beforeGet: once(func() { a.write("pages/p_1.json", typed) })}
	rep := a.sync()
	if got, _ := a.read("pages/p_1.json"); got != typed {
		t.Fatalf("the sync wrote over what was typed during it: %s", got)
	}
	if len(rep.Downloaded) != 0 || len(rep.Deferred) != 1 || rep.Deferred[0] != "pages/p_1.json" {
		t.Fatalf("the page should be left for the next sync: %+v", rep)
	}

	// The next sync sees both versions and merges them.
	a.syncer.Remote = remote
	rep = a.sync()
	got, _ := a.read("pages/p_1.json")
	if want := canon(t, page(map[string]string{"b1": "one", "b2": "from B", "b3": "typed on A"})); got != want || len(rep.Merged) != 1 {
		t.Fatalf("merged page = %s, report %+v", got, rep)
	}
}

func TestAFileEditedDuringTheSyncIsNotMovedToTheTrash(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	b := newDevice(t, "B", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	a.write("pages/p_2.json", page(map[string]string{"b1": "two"}))
	a.sync()
	b.sync()
	// B changes the first page and removes the second.
	b.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "from B"}))
	b.remove("pages/p_2.json")
	b.sync()

	// A is at work on the first page when the person edits the second.
	edited := page(map[string]string{"b1": "two", "b2": "kept on A"})
	a.syncer.Remote = &meanwhile{Remote: remote, beforeGet: once(func() { a.write("pages/p_2.json", edited) })}
	rep := a.sync()
	if got, ok := a.read("pages/p_2.json"); !ok || got != edited || len(rep.Trashed) != 0 {
		t.Fatalf("the edited page was trashed: %q %+v", got, rep)
	}
	// Edited after it was removed elsewhere, it comes back, as before.
	a.syncer.Remote = remote
	a.sync()
	if got := remoteRead(t, remote, "pages/p_2.json"); !strings.Contains(got, "kept on A") {
		t.Fatalf("remote page = %s", got)
	}
}

func TestAnUploadDoesNotReplaceWhatAnotherDeviceJustWrote(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	b := newDevice(t, "B", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	a.sync()
	b.sync()
	a.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "from A"}))
	b.write("pages/p_1.json", page(map[string]string{"b1": "one", "b3": "from B"}))

	// B syncs after A listed the folder, and before A's upload.
	a.syncer.Remote = &meanwhile{Remote: remote, beforePut: once(func() { b.sync() })}
	rep := a.sync()
	if got := remoteRead(t, remote, "pages/p_1.json"); !strings.Contains(got, "from B") {
		t.Fatalf("A replaced what B had just written: %s", got)
	}
	if len(rep.Uploaded) != 0 || len(rep.Deferred) != 1 {
		t.Fatalf("A's upload should be left for the next sync: %+v", rep)
	}

	a.syncer.Remote = remote
	a.sync()
	b.sync()
	want := canon(t, page(map[string]string{"b1": "one", "b2": "from A", "b3": "from B"}))
	gotA, _ := a.read("pages/p_1.json")
	gotB, _ := b.read("pages/p_1.json")
	if gotA != want || gotB != want {
		t.Fatalf("both devices should hold both changes:\nA %s\nB %s", gotA, gotB)
	}
}

func TestAFileChangedRemotelyDuringTheSyncIsNotMovedToTheRemoteTrash(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	b := newDevice(t, "B", remote)
	a.write("attachments/a.txt", "first")
	a.write("attachments/z.txt", "old")
	a.sync()
	b.sync()
	a.write("attachments/a.txt", "second")
	a.remove("attachments/z.txt")
	b.write("attachments/z.txt", "changed on B")

	// B uploads its change to z after A listed the folder.
	a.syncer.Remote = &meanwhile{Remote: remote, beforePut: once(func() { b.sync() })}
	rep := a.sync()
	if got := remoteRead(t, remote, "attachments/z.txt"); got != "changed on B" || len(rep.Trashed) != 0 {
		t.Fatalf("B's change was moved to the trash: %q %+v", got, rep)
	}
}

// A share that is not mounted, or a folder that was emptied, lists nothing:
// the vault must not follow it into the trash.
func TestAnEmptyRemoteFolderDoesNotEmptyTheVault(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	files := map[string]string{
		".notes/vault.json":         `{"id":"v_1"}`,
		"pages/p_1.json":            page(map[string]string{"b1": "one"}),
		"pages/p_2.json":            page(map[string]string{"b1": "two"}),
		"attachments/p_1/photo.png": "PNG",
	}
	for p, content := range files {
		a.write(p, content)
	}
	a.sync()
	emptyFolder(t, remote.Root)

	rep := a.sync()
	for p, want := range files {
		if got, ok := a.read(p); !ok || got != want {
			t.Fatalf("%s left the vault: %q", p, got)
		}
	}
	if len(rep.Trashed) != 0 || len(rep.HeldLocal) != len(files) || len(rep.HeldRemote) != 0 {
		t.Fatalf("the deletions should be held: %+v", rep)
	}
	// Held, they are asked about again by every run until someone decides.
	if rep = a.sync(); len(rep.HeldLocal) != len(files) {
		t.Fatalf("the second run forgot the question: %+v", rep)
	}

	// "Keep mine": the files go back to the remote folder.
	a.syncer.Deletions = RestoreDeleted
	rep = a.sync()
	if len(rep.Uploaded) != len(files) || len(rep.HeldLocal) != 0 {
		t.Fatalf("restoring should upload every file: %+v", rep)
	}
	for p, want := range files {
		if got := remoteRead(t, remote, p); got != want {
			t.Fatalf("remote %s = %q", p, got)
		}
	}
	a.syncer.Deletions = HoldManyDeletions
	if rep = a.sync(); len(rep.Uploaded)+len(rep.Downloaded)+len(rep.Trashed)+len(rep.HeldLocal) != 0 {
		t.Fatalf("nothing is left to do: %+v", rep)
	}
}

func TestHeldDeletionsAreAppliedWhenThePersonSaysSo(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	a.write("pages/p_2.json", page(map[string]string{"b1": "two"}))
	a.sync()
	emptyFolder(t, remote.Root)
	if rep := a.sync(); len(rep.HeldLocal) != 2 {
		t.Fatalf("held = %+v", rep)
	}
	a.syncer.Deletions = ApplyDeletions
	rep := a.sync()
	if len(rep.Trashed) != 2 || len(rep.HeldLocal) != 0 {
		t.Fatalf("applied = %+v", rep)
	}
	if _, ok := a.read("pages/p_1.json"); ok {
		t.Fatal("the page should be in the trash")
	}
	if trash, _ := ListTrash(a.root); len(trash) != 2 {
		t.Fatalf("trash = %v", trash)
	}
}

func TestAnEmptiedVaultDoesNotEmptyTheRemoteFolder(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	a.write("pages/p_2.json", page(map[string]string{"b1": "two"}))
	a.sync()
	emptyFolder(t, a.root)

	rep := a.sync()
	if len(rep.Trashed) != 0 || len(rep.HeldRemote) != 2 {
		t.Fatalf("the deletions should be held: %+v", rep)
	}
	if got := remoteRead(t, remote, "pages/p_1.json"); !strings.Contains(got, "one") {
		t.Fatalf("remote page = %s", got)
	}
	a.syncer.Deletions = RestoreDeleted
	if rep = a.sync(); len(rep.Downloaded) != 2 {
		t.Fatalf("restoring should download both pages: %+v", rep)
	}
	if got, ok := a.read("pages/p_2.json"); !ok || !strings.Contains(got, "two") {
		t.Fatalf("local page = %q", got)
	}
}

func TestManyDeletionsAreHeldAndAFewGoThrough(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	b := newDevice(t, "B", remote)
	for i := 0; i < 20; i++ {
		a.write(fmt.Sprintf("pages/p_%02d.json", i), page(map[string]string{"b1": fmt.Sprint(i)}))
	}
	a.sync()
	b.sync()

	// A few pages removed on B are removed on A too.
	for i := 0; i < 3; i++ {
		b.remove(fmt.Sprintf("pages/p_%02d.json", i))
	}
	if rep := b.sync(); len(rep.Trashed) != 3 || len(rep.HeldRemote) != 0 {
		t.Fatalf("three deletions should go through on B: %+v", rep)
	}
	if rep := a.sync(); len(rep.Trashed) != 3 || len(rep.HeldLocal) != 0 {
		t.Fatalf("three deletions should go through on A: %+v", rep)
	}

	// Most of what is left removed at once is held, on the device that
	// removed them and on the one that would follow.
	for i := 3; i < 15; i++ {
		b.remove(fmt.Sprintf("pages/p_%02d.json", i))
	}
	rep := b.sync()
	if len(rep.Trashed) != 0 || len(rep.HeldRemote) != 12 {
		t.Fatalf("twelve of seventeen should be held on B: %+v", rep)
	}
	b.syncer.Deletions = ApplyDeletions
	if rep = b.sync(); len(rep.Trashed) != 12 {
		t.Fatalf("B said so: %+v", rep)
	}
	rep = a.sync()
	if len(rep.Trashed) != 0 || len(rep.HeldLocal) != 12 {
		t.Fatalf("twelve of seventeen should be held on A: %+v", rep)
	}
	if _, ok := a.read("pages/p_03.json"); !ok {
		t.Fatal("a held page left the vault")
	}
}

// The vault thins out a page's old versions by itself: that is not a person
// removing files, however many go at once.
func TestOldVersionsThinnedOutAreNotHeld(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	for i := 0; i < 30; i++ {
		a.write(fmt.Sprintf(".notes/history/p_1/v_%02d.json", i), `{"parts":{}}`)
	}
	a.sync()
	for i := 0; i < 25; i++ {
		a.remove(fmt.Sprintf(".notes/history/p_1/v_%02d.json", i))
	}
	if rep := a.sync(); len(rep.Trashed) != 25 || len(rep.HeldRemote) != 0 {
		t.Fatalf("thinned versions should go through: %d trashed, %d held", len(rep.Trashed), len(rep.HeldRemote))
	}
}

func TestAPageIsKeptAsItWasBeforeASyncReplacesIt(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "A", remote)
	b := newDevice(t, "B", remote)
	backups := filepath.Join(t.TempDir(), "backup")
	a.syncer.BackupDir = backups
	first := page(map[string]string{"b1": "one"})
	a.write("pages/p_1.json", first)
	a.write("attachments/photo.png", "PNG")
	a.sync()
	b.sync()
	if _, err := os.Stat(backups); !os.IsNotExist(err) {
		t.Fatal("an upload replaces nothing here, and keeps nothing")
	}

	// A download over the page keeps the page as it was.
	b.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "from B"}))
	b.write("attachments/photo.png", "PNG2")
	b.sync()
	a.sync()
	kept := backupsOf(t, backups)
	if len(kept) != 1 || kept[0].content != first || !strings.HasPrefix(kept[0].rel, "2026-11-10/pages/p_1.") {
		t.Fatalf("backups after a download = %+v", kept)
	}

	// A merge keeps the local page as it was, too.
	local := page(map[string]string{"b1": "one", "b2": "from B", "b3": "from A"})
	a.write("pages/p_1.json", local)
	b.write("pages/p_1.json", page(map[string]string{"b1": "ONE", "b2": "from B"}))
	b.sync()
	a.syncer.Now = func() time.Time { return fixedNow.Add(time.Minute) }
	if rep := a.sync(); len(rep.Merged) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	kept = backupsOf(t, backups)
	if len(kept) != 2 || kept[1].content != local {
		t.Fatalf("backups after a merge = %+v", kept)
	}

	// Thirty days on, the copies are gone.
	a.syncer.Now = func() time.Time { return fixedNow.AddDate(0, 0, BackupDays+1) }
	a.sync()
	if kept = backupsOf(t, backups); len(kept) != 0 {
		t.Fatalf("old backups stay: %+v", kept)
	}
}

type keptFile struct{ rel, content string }

func backupsOf(t *testing.T, dir string) []keptFile {
	t.Helper()
	var out []keptFile
	err := filepath.WalkDir(dir, func(full string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, full)
		out = append(out, keptFile{filepath.ToSlash(rel), string(data)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
