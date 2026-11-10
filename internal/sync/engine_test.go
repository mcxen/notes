package sync

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notes/internal/storage"
	"notes/internal/vault"
)

var fixedNow = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// device is one machine: a vault folder with its own manifest and base copies.
type device struct {
	t      *testing.T
	name   string
	root   string
	syncer *Syncer
}

func newDevice(t *testing.T, name string, remote storage.Remote) *device {
	t.Helper()
	dir := t.TempDir()
	d := &device{t: t, name: name, root: filepath.Join(dir, "vault")}
	d.syncer = &Syncer{
		Root:         d.root,
		BaseDir:      filepath.Join(dir, "base"),
		ManifestPath: filepath.Join(dir, "manifest.json"),
		Remote:       remote,
		Device:       name,
		Now:          func() time.Time { return fixedNow },
	}
	if err := os.MkdirAll(d.root, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d *device) write(rel, content string) {
	d.t.Helper()
	full := filepath.Join(d.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) remove(rel string) {
	d.t.Helper()
	if err := os.Remove(filepath.Join(d.root, filepath.FromSlash(rel))); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) read(rel string) (string, bool) {
	d.t.Helper()
	data, err := os.ReadFile(filepath.Join(d.root, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		d.t.Fatal(err)
	}
	return string(data), true
}

func (d *device) sync() Report {
	d.t.Helper()
	rep, err := d.syncer.Run(context.Background())
	if err != nil {
		d.t.Fatalf("%s: sync: %v", d.name, err)
	}
	if len(rep.Failed) > 0 {
		d.t.Fatalf("%s: files failed: %v", d.name, rep.Failed)
	}
	return rep
}

// page is a page file with the given paragraphs, as the vault writes them.
func page(blocks map[string]string) string {
	var parts []string
	for _, id := range []string{"b1", "b2", "b3"} {
		if text, ok := blocks[id]; ok {
			parts = append(parts, `"`+id+`":{"kind":"text","text":"`+text+`"}`)
		}
	}
	return `{"blocks":{` + strings.Join(parts, ",") + `},"format":1,"page":{"id":"p_1","title":"Notes"}}`
}

// canon writes a page the way the vault does, so pages compare by content.
func canon(t *testing.T, text string) string {
	t.Helper()
	obj, err := vault.DecodeObject([]byte(text))
	if err != nil {
		t.Fatalf("canon: %v", err)
	}
	data, err := vault.EncodeObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func remoteRead(t *testing.T, remote storage.Remote, rel string) string {
	t.Helper()
	data, err := remote.Get(context.Background(), rel)
	if err != nil {
		t.Fatalf("remote %s: %v", rel, err)
	}
	return string(data)
}

func newRemote(t *testing.T) storage.Dir {
	t.Helper()
	return storage.Dir{Root: t.TempDir()}
}

func TestFirstSyncUploadsAndTheOtherDeviceDownloads(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "two"}))
	a.write(".notes/vault.json", `{"format":1,"id":"v_1"}`)
	rep := a.sync()
	if len(rep.Uploaded) != 2 {
		t.Fatalf("uploaded = %v", rep.Uploaded)
	}
	if got := remoteRead(t, remote, "pages/p_1.json"); got != page(map[string]string{"b1": "one", "b2": "two"}) {
		t.Fatalf("remote page = %s", got)
	}
	b.sync()
	if got, _ := b.read("pages/p_1.json"); got != page(map[string]string{"b1": "one", "b2": "two"}) {
		t.Fatalf("second device page = %s", got)
	}
	if rep := a.sync(); len(rep.Uploaded)+len(rep.Downloaded)+len(rep.Merged)+len(rep.Trashed) != 0 {
		t.Fatalf("an unchanged round must do nothing: %+v", rep)
	}
}

func TestPageEditedOnBothSidesMergesBlockByBlock(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "two"}))
	a.sync()
	b.sync()

	// Both edit offline: A the first paragraph, B the second.
	a.write("pages/p_1.json", page(map[string]string{"b1": "A-edit", "b2": "two"}))
	a.sync()
	b.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "B-edit"}))
	rep := b.sync()
	if len(rep.Merged) != 1 || len(rep.Conflicts) != 0 {
		t.Fatalf("want one merge and no conflict, got merged=%v conflicts=%v", rep.Merged, rep.Conflicts)
	}
	want := canon(t, page(map[string]string{"b1": "A-edit", "b2": "B-edit"}))
	if got, _ := b.read("pages/p_1.json"); got != want {
		t.Fatalf("merged on the second device = %s", got)
	}
	if got := remoteRead(t, remote, "pages/p_1.json"); got != want {
		t.Fatalf("merged on the remote = %s", got)
	}
	a.sync()
	gotA, _ := a.read("pages/p_1.json")
	gotB, _ := b.read("pages/p_1.json")
	if gotA != gotB {
		t.Fatalf("both devices must end with the same page:\n%s\n---\n%s", gotA, gotB)
	}
}

func TestSameFieldChangedOnBothSidesKeepsLocalAndReportsIt(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "one"}))
	a.sync()
	b.sync()

	a.write("pages/p_1.json", page(map[string]string{"b1": "from Mac"}))
	a.sync()
	b.write("pages/p_1.json", page(map[string]string{"b1": "from PC"}))
	rep := b.sync()
	if len(rep.Conflicts) != 1 || rep.Conflicts[0].Path != "pages/p_1.json" {
		t.Fatalf("conflicts = %+v", rep.Conflicts)
	}
	if got := strings.Join(rep.Conflicts[0].Fields, ","); got != "blocks.b1.text" {
		t.Fatalf("fields = %s", got)
	}
	if got, _ := b.read("pages/p_1.json"); !strings.Contains(got, "from PC") {
		t.Fatalf("the local value must stay: %s", got)
	}
	copyText, ok := b.read(rep.Conflicts[0].Copy)
	if !ok || !strings.Contains(copyText, "from Mac") {
		t.Fatalf("the other version must be kept as a checkpoint file: %q", copyText)
	}
	a.sync()
	if got, _ := a.read("pages/p_1.json"); !strings.Contains(got, "from PC") {
		t.Fatalf("the first device takes the merged page: %s", got)
	}
}

func TestAttachmentChangedOnBothSidesIsKeptTwice(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("attachments/owner/x.bin", "one")
	a.sync()
	b.sync()

	a.write("attachments/owner/x.bin", "A-bytes")
	a.sync()
	b.write("attachments/owner/x.bin", "B-bytes")
	rep := b.sync()
	if len(rep.Conflicts) != 1 {
		t.Fatalf("want one conflict, got %+v", rep.Conflicts)
	}
	copyPath := rep.Conflicts[0].Copy
	if copyPath != "attachments/owner/x (冲突 PC 2026-11-10).bin" {
		t.Fatalf("copy name = %q", copyPath)
	}
	if got, _ := b.read("attachments/owner/x.bin"); got != "B-bytes" {
		t.Fatalf("local version = %q", got)
	}
	if got, _ := b.read(copyPath); got != "A-bytes" {
		t.Fatalf("other version = %q", got)
	}
	a.sync()
	if got, _ := a.read("attachments/owner/x.bin"); got != "B-bytes" {
		t.Fatalf("first device, path = %q", got)
	}
	if got, _ := a.read(copyPath); got != "A-bytes" {
		t.Fatalf("first device, copy = %q", got)
	}
}

func TestRemovedFileIsTrashedOnBothSides(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("notes/todo.txt", "buy milk")
	a.sync()
	b.sync()

	a.remove("notes/todo.txt")
	rep := a.sync()
	if len(rep.Trashed) != 1 {
		t.Fatalf("deleting here moves the remote file to its trash: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(remote.Root, "notes", "todo.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the remote file must be gone from its place")
	}
	if got := remoteRead(t, remote, ".trash/2026-11-10/notes/todo.txt"); got != "buy milk" {
		t.Fatalf("remote trash = %q", got)
	}
	rep = b.sync()
	if len(rep.Trashed) != 1 {
		t.Fatalf("the other device trashes its copy: %+v", rep)
	}
	if _, ok := b.read("notes/todo.txt"); ok {
		t.Fatal("the other device must not keep the file in place")
	}
	if got, _ := b.read(".trash/2026-11-10/notes/todo.txt"); got != "buy milk" {
		t.Fatalf("local trash = %q", got)
	}
}

func TestPageRemovedRemotelyIsTrashedLocallyWhenUnchanged(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "x"}))
	a.sync()
	b.sync()
	a.remove("pages/p_1.json")
	a.sync()
	rep := b.sync()
	if len(rep.Trashed) != 1 {
		t.Fatalf("b should trash the unchanged page: %+v", rep)
	}
}

func TestRemotelyDeletedPageEditedHereIsKept(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("pages/p_1.json", page(map[string]string{"b1": "x"}))
	a.sync()
	b.sync()
	a.remove("pages/p_1.json")
	a.sync()
	b.write("pages/p_1.json", page(map[string]string{"b1": "edited"}))
	b.sync()
	if got := remoteRead(t, remote, "pages/p_1.json"); !strings.Contains(got, "edited") {
		t.Fatalf("an edit made after a remote delete must come back: %s", got)
	}
}

func TestNewFilesOnEitherSideAreCarriedOver(t *testing.T) {
	remote := newRemote(t)
	a := newDevice(t, "Mac", remote)
	b := newDevice(t, "PC", remote)
	a.write("attachments/a.png", "aaa")
	a.sync()
	b.write("attachments/b.png", "bbb")
	b.sync()
	a.sync()
	if got, _ := a.read("attachments/b.png"); got != "bbb" {
		t.Fatalf("a should receive b's new file, got %q", got)
	}
	if got := remoteRead(t, remote, "attachments/a.png"); got != "aaa" {
		t.Fatalf("remote lost a's file: %q", got)
	}
}

func TestFailingFileIsReportedAndTheOthersStillSync(t *testing.T) {
	inner := newRemote(t)
	remote := &failingRemote{Remote: inner, fail: "bad.bin"}
	a := newDevice(t, "Mac", remote)
	a.write("attachments/bad.bin", "x")
	a.write("attachments/good.bin", "y")
	rep, err := a.syncer.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 1 || !strings.HasPrefix(rep.Failed[0], "attachments/bad.bin:") {
		t.Fatalf("failed = %v", rep.Failed)
	}
	if got := remoteRead(t, inner, "attachments/good.bin"); got != "y" {
		t.Fatalf("the other file must still sync: %q", got)
	}
}

type failingRemote struct {
	storage.Remote
	fail string
}

// Put fails for any file whose name contains fail: uploads go through a
// temporary name first, so the name is what matches.
func (f *failingRemote) Put(ctx context.Context, p string, data []byte) (storage.Entry, error) {
	if strings.Contains(path.Base(p), f.fail) {
		return storage.Entry{}, errors.New("disk refused")
	}
	return f.Remote.Put(ctx, p, data)
}

func TestMergePageRules(t *testing.T) {
	base := []byte(page(map[string]string{"b1": "one", "b2": "two"}))
	local := []byte(page(map[string]string{"b1": "one", "b2": "L"}))
	remote := []byte(page(map[string]string{"b1": "R", "b2": "two", "b3": "new"}))
	merged, fields, err := MergePage(base, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if want := canon(t, page(map[string]string{"b1": "R", "b2": "L", "b3": "new"})); string(merged) != want {
		t.Fatalf("merged = %s\nwant    %s", merged, want)
	}
	if len(fields) != 0 {
		t.Fatalf("no field changed on both sides, fields = %v", fields)
	}

	// The same field on both sides: local wins, and the field is reported.
	merged, fields, err = MergePage(base, []byte(page(map[string]string{"b1": "L1", "b2": "two"})), []byte(page(map[string]string{"b1": "R1", "b2": "two"})))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), `"text": "L1"`) || len(fields) != 1 || fields[0] != "blocks.b1.text" {
		t.Fatalf("merged = %s fields = %v", merged, fields)
	}

	// A paragraph one side deleted and the other left alone is deleted.
	merged, _, err = MergePage(base, []byte(page(map[string]string{"b1": "one"})), base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(merged), "b2") {
		t.Fatalf("deletion must win over an unchanged paragraph: %s", merged)
	}

	// Without a base both pages are unioned.
	merged, _, err = MergePage(nil, []byte(page(map[string]string{"b1": "one"})), []byte(page(map[string]string{"b2": "two"})))
	if err != nil {
		t.Fatal(err)
	}
	if want := canon(t, page(map[string]string{"b1": "one", "b2": "two"})); string(merged) != want {
		t.Fatalf("no base merge = %s", merged)
	}
}

func TestConflictNamesKeepTheExtension(t *testing.T) {
	s := &Syncer{Device: "Mac/Book", Now: func() time.Time { return fixedNow }}
	if got := s.conflictName("attachments/a/photo.png"); got != "attachments/a/photo (冲突 Mac-Book 2026-11-10).png" {
		t.Fatalf("conflictName = %q", got)
	}
	if got := s.conflictName("README"); got != "README (冲突 Mac-Book 2026-11-10)" {
		t.Fatalf("conflictName without extension = %q", got)
	}
	if got := s.checkpointName("pages/p_1.json"); got != ".notes/conflicts/p_1 (冲突 Mac-Book 2026-11-10).json" {
		t.Fatalf("checkpointName = %q", got)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	m, err := LoadManifest(path)
	if err != nil || len(m.Files) != 0 {
		t.Fatalf("missing manifest should be empty: %v", err)
	}
	m.Files["pages/p_1.json"] = Entry{Base: "abc", Tag: "t", LocalSize: 3, LocalMod: 9}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Files["pages/p_1.json"] != (Entry{Base: "abc", Tag: "t", LocalSize: 3, LocalMod: 9}) {
		t.Fatalf("round trip = %+v", got.Files)
	}
}
