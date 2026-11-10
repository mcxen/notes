package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTemp(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	v, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, dir
}

func mustWrite(t *testing.T, v *Vault, w Write) Ack {
	t.Helper()
	ack, err := v.Write(w)
	if err != nil {
		t.Fatalf("write %s %s: %v", w.Collection, w.ID, err)
	}
	return ack
}

func readObject(t *testing.T, path string) Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	obj, err := decodeObject(data)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return obj
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func snapshotText(t *testing.T, v *Vault) string {
	t.Helper()
	return jsonOf(t, v.Snapshot().Records)
}

func TestOpenCreatesLayoutAndKeepsID(t *testing.T) {
	dir := t.TempDir()
	v1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := v1.Info().ID
	if !ValidID(id) {
		t.Fatalf("vault id %q is not valid", id)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"pages", "attachments", ".notes/history", ".notes/vault.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	v2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v2.Close()
	if v2.Info().ID != id {
		t.Fatalf("reopened vault id = %q, want %q", v2.Info().ID, id)
	}
}

func TestWriteFlushAndReopen(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1", "title": "Hello", "parent_id": "p_root"}})
	ack := mustWrite(t, v, Write{Action: "put", Collection: Blocks, ID: BlockRecordID("p_1", "b_1"), Value: Record{"page": "p_1", "kind": "text", "order": 1, "content": []any{}}})
	if ack.Value["page"] != "p_1" {
		t.Fatalf("paragraph ack should name its page, got %v", ack.Value)
	}
	if !v.Pending() {
		t.Fatal("writes should be pending until flushed")
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if v.Pending() {
		t.Fatal("flush should clear pending writes")
	}

	file := readObject(t, filepath.Join(dir, "pages", "p_1.json"))
	if file["format"] != json.Number("1") {
		t.Fatalf("format = %v", file["format"])
	}
	blocks := file["blocks"].(map[string]any)
	block := blocks["b_1"].(map[string]any)
	if _, has := block["page"]; has {
		t.Fatal("a paragraph is stored under its page, without naming it")
	}

	before := snapshotText(t, v)
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if after := snapshotText(t, reopened); after != before {
		t.Fatalf("reopened snapshot differs:\n%s\n%s", before, after)
	}
}

func TestPatchMergesFieldsAndNullRemoves(t *testing.T) {
	v, _ := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1", "title": "A", "icon": "x", "meta": map[string]any{"a": 1}}})
	ack := mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_1", Patch: Record{"title": "B", "icon": nil, "meta": map[string]any{"b": 2}}})
	want := `{"id":"p_1","meta":{"a":1,"b":2},"title":"B"}`
	if got := jsonOf(t, ack.Value); got != want {
		t.Fatalf("patched record = %s, want %s", got, want)
	}
	created := mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_2", Patch: Record{"id": "p_2"}})
	if created.Value["id"] != "p_2" {
		t.Fatalf("a patch of a new record starts from nothing: %v", created.Value)
	}
}

func TestDeleteRemovesEmptyPageFile(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}})
	mustWrite(t, v, Write{Action: "put", Collection: Blocks, ID: BlockRecordID("p_1", "b_1"), Value: Record{"page": "p_1", "kind": "text"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pages", "p_1.json")
	mustWrite(t, v, Write{Action: "delete", Collection: Blocks, ID: BlockRecordID("p_1", "b_1")})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, has := readObject(t, path)["blocks"].(map[string]any)["b_1"]; has {
		t.Fatal("deleted paragraph is still on disk")
	}
	mustWrite(t, v, Write{Action: "delete", Collection: Pages, ID: "p_1"})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("page file should be removed once nothing is left in it, stat err = %v", err)
	}
}

func TestUnchangedWriteIsNotPending(t *testing.T) {
	v, _ := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_1", Patch: Record{"id": "p_1"}})
	if v.Pending() {
		t.Fatal("a patch that changes nothing should not dirty the page")
	}
}

func TestWriteIsDebouncedUntilFlush(t *testing.T) {
	v, _ := openTemp(t)
	v.flushWait = 1 << 40 // effectively never: the test decides when to flush
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}})
	mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_1", Patch: Record{"title": "x"}})
	if v.timer == nil {
		t.Fatal("a write should arm the quiet-period flush")
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if v.timer != nil || v.Pending() {
		t.Fatal("flush should disarm the timer and clear pending writes")
	}
}

func TestRejectsInvalidRecordsAndPaths(t *testing.T) {
	v, _ := openTemp(t)
	cases := []Write{
		{Action: "put", Collection: Pages, ID: "../escape", Value: Record{}},
		{Action: "put", Collection: Pages, ID: "", Value: Record{}},
		{Action: "put", Collection: Blocks, ID: "not-a-block", Value: Record{}},
		{Action: "put", Collection: Blocks, ID: BlockRecordID("p_1", "b_1"), Value: Record{"page": "p_2"}},
		{Action: "put", Collection: Versions, ID: "junk", Value: Record{}},
		{Action: "put", Collection: Settings, ID: "other", Value: Record{}},
		{Action: "put", Collection: "unknown", ID: "x", Value: Record{}},
		{Action: "put", Collection: Pages, ID: "p_1"},
		{Action: "explode", Collection: Pages, ID: "p_1"},
	}
	for _, c := range cases {
		if _, err := v.Write(c); err == nil {
			t.Errorf("write %+v should be refused", c)
		}
	}
	if len(v.Snapshot().Records) != 0 {
		t.Fatal("refused writes must not change the vault")
	}
}

func TestAttachmentPathsStayUnderAttachments(t *testing.T) {
	v, dir := openTemp(t)
	if err := v.WriteFile("files/owner_1/photo-1.png", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "attachments", "owner_1", "photo-1.png"))
	if err != nil || len(got) != 3 {
		t.Fatalf("attachment not stored under attachments/: %v", err)
	}
	read, err := v.ReadFile("files/owner_1/photo-1.png")
	if err != nil || len(read) != 3 {
		t.Fatalf("read back: %v", err)
	}
	for _, bad := range []string{"photo.png", "files/../pages/p_1.json", "files/a/../../x", "files//x", "files/a b.png", "../files/x"} {
		if err := v.WriteFile(bad, []byte{1}); err == nil {
			t.Errorf("WriteFile(%q) should be refused", bad)
		}
	}
}

func TestLeftoverTemporaryFilesAreRemoved(t *testing.T) {
	dir := t.TempDir()
	v, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
	leftover := filepath.Join(dir, "pages", ".p_1.json.tmp-123456")
	if err := os.WriteFile(leftover, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := os.Stat(leftover); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("interrupted save should be cleaned up, stat err = %v", err)
	}
}

func TestReconcileMergesExternalEditAndKeepsLocalPending(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1", "title": "A"}})
	mustWrite(t, v, Write{Action: "put", Collection: Blocks, ID: BlockRecordID("p_1", "b_1"), Value: Record{"page": "p_1", "kind": "text", "text": "one"}})
	mustWrite(t, v, Write{Action: "put", Collection: Blocks, ID: BlockRecordID("p_1", "b_2"), Value: Record{"page": "p_1", "kind": "text", "text": "two"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	// A local edit not yet saved.
	mustWrite(t, v, Write{Action: "patch", Collection: Blocks, ID: BlockRecordID("p_1", "b_1"), Patch: Record{"text": "local"}})

	// Another program edits the file: title, b_2, and a new b_3.
	external := `{"blocks":{"b_1":{"kind":"text","text":"one"},"b_2":{"kind":"text","text":"EXT"},"b_3":{"kind":"text","text":"three"}},"format":1,"page":{"id":"p_1","title":"B"}}`
	if err := os.WriteFile(filepath.Join(dir, "pages", "p_1.json"), []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := v.Reconcile("pages/p_1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 3 {
		t.Fatalf("want 3 changes (title, b_2, b_3), got %+v", changes.Changes)
	}
	text := snapshotText(t, v)
	for _, want := range []string{`"text":"local"`, `"text":"EXT"`, `"text":"three"`, `"title":"B"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("snapshot lacks %s: %s", want, text)
		}
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	onDisk := readObject(t, filepath.Join(dir, "pages", "p_1.json"))
	if !strings.Contains(jsonOf(t, onDisk), `"text":"local"`) {
		t.Fatal("the local pending edit must reach the disk, not be overwritten by the external file")
	}
}

func TestReconcileIgnoresItsOwnWrites(t *testing.T) {
	v, _ := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	changes, err := v.Reconcile("pages/p_1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 0 {
		t.Fatalf("a file this process wrote must not be read as a change: %+v", changes.Changes)
	}
}

func TestReconcileRemovedFileForgetsItsRecords(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "pages", "p_1.json")); err != nil {
		t.Fatal(err)
	}
	changes, err := v.Reconcile("pages/p_1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 1 || !changes.Changes[0].Deleted {
		t.Fatalf("want one deletion, got %+v", changes.Changes)
	}
	if len(v.Snapshot().Records) != 0 {
		t.Fatal("removed page must be gone from memory")
	}
}

func TestReconcileAllFindsChangesAcrossFiles(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pages", "p_9.json"), []byte(`{"format":1,"page":{"id":"p_9"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := v.ReconcileAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].ID != "p_9" {
		t.Fatalf("want the new page p_9, got %+v", changes.Changes)
	}
}

func TestHistoryPartsRoundTrip(t *testing.T) {
	v, dir := openTemp(t)
	part := func(k int) Record {
		return Record{"page": "p_1", "version": "v_1", "generation": "g1", "created_at": "2026-11-10", "part": k, "parts": 2, "payload": "x"}
	}
	mustWrite(t, v, Write{Action: "put", Collection: Versions, ID: HistoryRecordID("p_1", "v_1", 0), Value: part(0)})
	mustWrite(t, v, Write{Action: "put", Collection: Versions, ID: HistoryRecordID("p_1", "v_1", 1), Value: part(1)})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".notes", "history", "p_1", "v_1.json")
	if parts := readObject(t, path)["parts"].(map[string]any); len(parts) != 2 {
		t.Fatalf("history file should hold both parts, got %d", len(parts))
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if n := len(reopened.Snapshot().Records); n != 2 {
		t.Fatalf("reopened vault has %d versions records, want 2", n)
	}
	mustWrite(t, v, Write{Action: "delete", Collection: Versions, ID: HistoryRecordID("p_1", "v_1", 1)})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if parts := readObject(t, path)["parts"].(map[string]any); len(parts) != 1 {
		t.Fatalf("after deleting part 1, %d parts remain", len(parts))
	}
}

func TestSettingsAreStoredInWorkspaceFile(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Settings, ID: "preferences", Value: Record{"theme": "dark"}})
	mustWrite(t, v, Write{Action: "put", Collection: Settings, ID: "roots", Value: Record{"roots": []any{"p_1"}}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	obj := readObject(t, filepath.Join(dir, ".notes", "workspace.json"))
	if obj["preferences"].(map[string]any)["theme"] != "dark" {
		t.Fatalf("preferences not stored: %v", obj)
	}
}

func TestRecordIDsRoundTrip(t *testing.T) {
	id := BlockRecordID("p_8f2k", "b_19ac")
	if id != "6_p_8f2k_b_19ac" {
		t.Fatalf("block id = %q", id)
	}
	pageID, blockID, ok := ParseBlockRecordID(id)
	if !ok || pageID != "p_8f2k" || blockID != "b_19ac" {
		t.Fatalf("parse = %q %q %v", pageID, blockID, ok)
	}
	for _, bad := range []string{"", "x_p_1_b", "10_abc_d", "3_abc_", "_abc_d", "2__x"} {
		if _, _, ok := ParseBlockRecordID(bad); ok {
			t.Errorf("ParseBlockRecordID(%q) should fail", bad)
		}
	}
	hid := HistoryRecordID("p_1", "v_1", 3)
	if hid != "3_p_1_v_1.part.3" {
		t.Fatalf("history id = %q", hid)
	}
	page, version, part, ok := ParseHistoryRecordID(hid)
	if !ok || page != "p_1" || version != "v_1" || part != 3 {
		t.Fatalf("history parse = %q %q %d %v", page, version, part, ok)
	}
	if _, version, part, _ := ParseHistoryRecordID(HistoryRecordID("p_1", "v_1", 0)); version != "v_1" || part != 0 {
		t.Fatal("part 0 has no suffix")
	}
}

func TestImportBackupMergesAndRejectsBadInput(t *testing.T) {
	v, dir := openTemp(t)
	pix := base64.StdEncoding.EncodeToString([]byte{0, 128, 255, 42})
	backup := `{
	  "format": "open-notes", "version": 1, "createdAt": "2026-11-10T00:00:00Z",
	  "records": [
	    {"scope":"personal","collection":"pages","id":"p_1","value":{"id":"p_1","title_content":[]}},
	    {"scope":"personal","collection":"blocks","id":"3_p_1_b_1","value":{"page":"p_1","kind":"text","order":1}},
	    {"scope":"personal","collection":"blocks","id":"3_p_2_b_9","value":{"page":"p_2","kind":"text","order":1}},
	    {"scope":"personal","collection":"blocks","id":"garbage","value":{"page":"p_1"}},
	    {"scope":"personal","collection":"versions","id":"3_p_1_v_1","value":{"page":"p_1","version":"v_1","part":0}},
	    {"scope":"personal","collection":"workspace_settings","id":"preferences","value":{"theme":"light"}}
	  ],
	  "files": [{"key":"personal\nfiles/owner_1/a.bin","type":"application/octet-stream","data":"` + pix + `"}]
	}`
	report, err := v.ImportBackup([]byte(backup))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if report.Pages != 1 || report.Blocks != 2 || report.Versions != 1 || report.Settings != 1 || report.Attachments != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Unplaced != 1 || report.Orphans != 1 {
		t.Fatalf("unplaced/orphans = %d/%d, want 1/1", report.Unplaced, report.Orphans)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "attachments", "owner_1", "a.bin")); err != nil || len(got) != 4 {
		t.Fatalf("attachment not imported: %v", err)
	}

	before := snapshotText(t, v)
	bad := []string{
		`{"format":"notes","version":1,"records":[],"files":[]}`,
		`{"format":"open-notes","version":1,"records":[{"scope":"space","collection":"pages","id":"p_1","value":{}}],"files":[]}`,
		`{"format":"open-notes","version":1,"records":[{"scope":"personal","collection":"pages","id":"p_1","value":{"id":"p_1"}}],"files":[{"key":"personal\n../x","type":"x","data":""}]}`,
		`not json`,
	}
	for _, text := range bad {
		if _, err := v.ImportBackup([]byte(text)); err == nil {
			t.Errorf("import of %q should fail", text)
		}
	}
	if after := snapshotText(t, v); after != before {
		t.Fatal("a rejected backup must not change the vault")
	}
}

func TestClosedVaultRefusesWrites(t *testing.T) {
	v, _ := openTemp(t)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Write(Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1"}}); err == nil {
		t.Fatal("a closed vault must refuse writes")
	}
}

func TestSerializationIsStable(t *testing.T) {
	v, dir := openTemp(t)
	mustWrite(t, v, Write{Action: "put", Collection: Pages, ID: "p_1", Value: Record{"id": "p_1", "title": "<b>&"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "pages", "p_1.json"))
	mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_1", Patch: Record{"title": "<b>&"}})
	mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_1", Patch: Record{"title": "other"}})
	mustWrite(t, v, Write{Action: "patch", Collection: Pages, ID: "p_1", Patch: Record{"title": "<b>&"}})
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "pages", "p_1.json"))
	if string(first) != string(second) {
		t.Fatalf("same content must give the same bytes:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(string(first), "<b>&") {
		t.Fatal("HTML characters are written as they are")
	}
}
