package mcpsvc

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"notes/internal/vault"
)

// toolResult is a tool's whole result: its content as well as its structure.
func (c *client) toolResult(name string, args map[string]any) map[string]any {
	c.t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args}, "Bearer "+c.key)
	result, ok := res["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s: no result: %v", name, res)
	}
	return result
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAPictureAndAFileGoOnAPageAndCanBeRead(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Files", "markdown": "Hello"})["id"].(string)

	pic := smallPNG(t)
	added := c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "my diagram (1).png", "data": base64.StdEncoding.EncodeToString(pic)})
	if added["type"] != "image/png" || added["name"] != "my diagram (1).png" || !strings.HasSuffix(added["path"].(string), "/my_diagram_1.png") {
		t.Fatalf("picture added: %v", added)
	}
	note := c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "todo.txt", "data": base64.StdEncoding.EncodeToString([]byte("buy milk"))})
	blob := c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "data.bin", "type": "application/x-thing", "data": base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})})

	// The bytes are in the vault's attachments, where the app looks.
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	full, err := v.AttachmentPath(added["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if onDisk, err := os.ReadFile(full); err != nil || !bytes.Equal(onDisk, pic) {
		t.Fatalf("on disk: %v", err)
	}
	_ = v.Close()

	fetched := c.fetch(map[string]any{"page_id": id})
	about := fetched["about"].(string)
	var listed []string
	if _, list, ok := strings.Cut(about, "Attachments:\n"); ok {
		listed = strings.Split(list, "\n")
	}
	if len(listed) != 3 {
		t.Fatalf("attachments: %q", about)
	}
	if !strings.HasPrefix(listed[0], "- my diagram (1).png (image/png, a picture, ") || !strings.HasSuffix(listed[0], " path: "+added["path"].(string)) {
		t.Fatalf("the picture: %q", listed[0])
	}
	if listed[1] != "- todo.txt (text/plain, 8 B) path: "+note["path"].(string) {
		t.Fatalf("the text file: %q", listed[1])
	}
	if !strings.Contains(fetched["markdown"].(string), "![my diagram (1).png]") || !strings.Contains(fetched["markdown"].(string), "[todo.txt]") {
		t.Fatalf("the Markdown names them: %q", fetched["markdown"])
	}

	// A picture is given as an image, with its bytes.
	got := c.toolResult("notes_read_attachment", map[string]any{"page_id": id, "path": added["path"]})
	content := got["content"].([]any)
	image := content[len(content)-1].(map[string]any)
	if image["type"] != "image" || image["mimeType"] != "image/png" {
		t.Fatalf("the picture: %v", image)
	}
	if raw, _ := base64.StdEncoding.DecodeString(image["data"].(string)); !bytes.Equal(raw, pic) {
		t.Fatal("the picture's bytes changed")
	}

	// A text file as text; any other file as a resource with its bytes.
	text := c.toolResult("notes_read_attachment", map[string]any{"page_id": id, "path": note["path"]})["content"].([]any)
	if last := text[len(text)-1].(map[string]any); last["type"] != "text" || last["text"] != "buy milk" {
		t.Fatalf("the text: %v", last)
	}
	other := c.toolResult("notes_read_attachment", map[string]any{"page_id": id, "path": blob["path"]})["content"].([]any)
	resource := other[len(other)-1].(map[string]any)
	if resource["type"] != "resource" {
		t.Fatalf("the bytes: %v", resource)
	}
	if raw, _ := base64.StdEncoding.DecodeString(resource["resource"].(map[string]any)["blob"].(string)); !bytes.Equal(raw, []byte{0, 1, 2, 3}) {
		t.Fatalf("the blob: %v", resource)
	}

	// Only what the page holds can be read: not another file in the vault.
	if res := c.tool("notes_read_attachment", map[string]any{"page_id": id, "path": "files/other/x/secret.txt"}); res["_error"] == nil {
		t.Fatalf("a path the page does not hold was read: %v", res)
	}
}

func TestAttachmentsFollowTheReadOnlySwitchAndTheSizeLimit(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Files"})["id"].(string)
	if res := c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "a.txt", "data": "!!not base64!!"}); res["_error"] == nil {
		t.Fatalf("bad base64: %v", res)
	}
	big := base64.StdEncoding.EncodeToString(make([]byte, maxAdd+1))
	if res := c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "a.bin", "data": big}); res["_error"] == nil {
		t.Fatalf("over the limit: %v", res)
	}

	ro := newClient(t, testHost{folders: []string{dir}, readOnly: true}, "sk-test")
	if res := ro.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "a.txt", "data": "aGk="}); res["_error"] == nil {
		t.Fatalf("read-only: %v", res)
	}
}

func TestAFileKeepsItsOwnNameInAnyScript(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Files"})["id"].(string)
	added := c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "会议纪要.pdf", "type": "application/pdf",
		"data": base64.StdEncoding.EncodeToString([]byte("%PDF-1.4"))})
	if added["name"] != "会议纪要.pdf" || !strings.HasSuffix(added["path"].(string), "/file.pdf") {
		t.Fatalf("added: %v", added)
	}
	fetched := c.fetch(map[string]any{"page_id": id})
	if about := fetched["about"].(string); !strings.Contains(about, "Attachments:\n- 会议纪要.pdf (application/pdf, 8 B) path: "+added["path"].(string)) {
		t.Fatalf("attachments: %q", about)
	}
	for name, want := range map[string]string{"报告 final.docx": "final.docx", "../../etc/passwd": "passwd", "a b.tar.gz": "a_b.tar.gz", "": "file"} {
		if got := safeName(name); got != want {
			t.Errorf("safeName(%q) = %q, want %q", name, got, want)
		}
	}
}
