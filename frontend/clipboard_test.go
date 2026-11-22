package frontend

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 60, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHasNotesInternalFormat(t *testing.T) {
	if !hasNotesInternalFormat([]string{"text/plain", "application/x-notes-blocks"}) {
		t.Error("expected Notes internal format to match")
	}
	if hasNotesInternalFormat([]string{"text/plain", "text/html"}) {
		t.Error("did not expect plain formats to match Notes internal format")
	}
}

func TestMakeImageThumbnail(t *testing.T) {
	raw := makeTestPNG(t, 400, 200)
	dataURL, w, h, ok := makeImageThumbnail(raw)
	if !ok || w != 400 || h != 200 {
		t.Fatalf("unexpected thumbnail metrics: ok=%v w=%d h=%d", ok, w, h)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("expected data URL prefix, got %q", dataURL[:min(len(dataURL), 30)])
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	thumb, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode thumb png: %v", err)
	}
	if thumb.Bounds().Dx() > clipboardThumbMaxW || thumb.Bounds().Dy() > clipboardThumbMaxH {
		t.Fatalf("thumbnail exceeds bounds: %v", thumb.Bounds())
	}

	if _, _, _, ok := makeImageThumbnail([]byte("not a png")); ok {
		t.Error("expected corrupt image bytes to fail")
	}
}

func TestClassifyAndBuildEntry(t *testing.T) {
	now := time.Date(2026, 11, 22, 9, 45, 0, 0, time.UTC)

	if _, ok := classifyAndBuildEntry("   \n\t ", "", nil, now); ok {
		t.Error("whitespace-only clipboard should be ignored")
	}
	token, ok := classifyAndBuildEntry("sk-proj-abcdefghijklmnopqrstuvwxyz123456", "", nil, now)
	if !ok || token.preview.Kind != "text" || token.payload.Text != "sk-proj-abcdefghijklmnopqrstuvwxyz123456" {
		t.Fatalf("expected API token to be captured normally: ok=%v entry=%+v", ok, token)
	}
	if _, ok := classifyAndBuildEntry("hello", `<div data-notes-blocks='{}'>hello</div>`, nil, now); ok {
		t.Error("Notes internal blocks HTML should be rejected")
	}

	single, ok := classifyAndBuildEntry("  hello   world  ", "", nil, now)
	if !ok || single.preview.Kind != "text" || single.preview.Lines != 0 || single.preview.Sample != "" || single.preview.Preview != "hello world" {
		t.Fatalf("unexpected single-line entry: ok=%v preview=%+v", ok, single.preview)
	}

	multi, ok := classifyAndBuildEntry("def greet():\r\n  return 'hi'\r\n  # end\r\n", "", nil, now)
	if !ok || multi.preview.Kind != "text" || multi.preview.Lines != 3 || multi.preview.Sample == "" {
		t.Fatalf("unexpected multi-line entry: ok=%v preview=%+v", ok, multi.preview)
	}

	u, ok := classifyAndBuildEntry("https://example.com/path?a=1", `<a href="https://example.com/path?a=1">https://example.com/path?a=1</a>`, nil, now)
	if !ok || u.preview.Kind != "url" || u.payload.HTML != "" {
		t.Fatalf("unexpected URL entry: ok=%v preview=%+v payload=%+v", ok, u.preview, u.payload)
	}

	h, ok := classifyAndBuildEntry("Title\nBody", "<h1>Title</h1><p>Body</p>", nil, now)
	if !ok || h.preview.Kind != "html" || h.payload.HTML == "" {
		t.Fatalf("unexpected HTML entry: ok=%v preview=%+v", ok, h.preview)
	}

	imgBytes := makeTestPNG(t, 400, 200)
	imgEntry, ok := classifyAndBuildEntry("", "", imgBytes, now)
	if !ok || imgEntry.preview.Kind != "image" || imgEntry.preview.Preview != "400×200" || imgEntry.preview.ImagePreview == "" || imgEntry.payload.Image == "" {
		t.Fatalf("unexpected image entry: ok=%v preview=%+v", ok, imgEntry.preview)
	}
}

func TestClipboardWatcherQueue(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	stopped := false
	var emitted []ClipboardState

	w := newClipboardWatcher(func(st ClipboardState) {
		emitted = append(emitted, st)
	})
	w.now = func() time.Time { return now }
	w.captureFn = func(bool) {}
	w.subscribe = func(func()) func() {
		return func() { stopped = true }
	}

	mustEntry := func(text string) clipboardEntry {
		e, ok := classifyAndBuildEntry(text, "", nil, now)
		if !ok {
			t.Fatalf("failed to build entry for %q", text)
		}
		return e
	}

	// Recording while disabled is ignored.
	w.recordEntry(mustEntry("ignored"))
	if len(w.state().Items) != 0 {
		t.Fatal("disabled watcher must ignore recorded items")
	}

	w.setEnabled(true)

	// Push 6 distinct entries -> queue caps at 5, newest first.
	for _, s := range []string{"one", "two", "three", "four", "five", "six"} {
		now = now.Add(time.Second)
		w.recordEntry(mustEntry(s))
	}
	st := w.state()
	if !st.Enabled || len(st.Items) != 5 {
		t.Fatalf("expected 5 items, got %+v", st)
	}
	if st.Items[0].Preview != "six" || st.Items[4].Preview != "two" {
		t.Fatalf("unexpected queue order: %+v", st.Items)
	}

	// Duplicate head is a no-op; re-recording an older entry promotes it to index 0.
	before := len(emitted)
	w.recordEntry(mustEntry("six"))
	if len(emitted) != before {
		t.Error("expected duplicate head to be ignored")
	}
	now = now.Add(time.Second)
	w.recordEntry(mustEntry("three"))
	st = w.state()
	if st.Items[0].Preview != "three" || len(st.Items) != 5 {
		t.Fatalf("expected 'three' promoted to head: %+v", st.Items)
	}

	// Dismiss top item ("three"); re-firing within <2s is skipped, >2s is accepted again.
	dismissedID := st.Items[0].ID
	w.dismiss(dismissedID)
	w.recordEntry(mustEntry("three"))
	if w.state().Items[0].Preview == "three" {
		t.Error("expected dismissed item not to re-echo immediately")
	}
	now = now.Add(3 * time.Second)
	w.recordEntry(mustEntry("three"))
	if w.state().Items[0].Preview != "three" {
		t.Error("expected explicit re-copy after 3s to be accepted")
	}

	// Take top item consumes it atomically.
	topID := w.state().Items[0].ID
	payload, ok := w.take(topID)
	if !ok || payload.Text != "three" {
		t.Fatalf("unexpected take result: ok=%v payload=%+v", ok, payload)
	}
	if _, ok := w.take(topID); ok {
		t.Error("second take on same ID should return false")
	}

	// consumeMatch removes matching text and also top image when text is empty.
	w.consumeMatch("four\r\n")
	for _, it := range w.state().Items {
		if it.Preview == "four" {
			t.Error("expected consumeMatch to remove 'four'")
		}
	}
	now = now.Add(3 * time.Second)
	imgEntry, ok := classifyAndBuildEntry("", "", makeTestPNG(t, 20, 20), now)
	if !ok {
		t.Fatal("expected image entry")
	}
	w.recordEntry(imgEntry)
	if w.state().Items[0].Kind != "image" {
		t.Fatal("expected image at head")
	}
	w.consumeMatch("")
	if w.state().Items[0].Kind == "image" {
		t.Error("expected empty consumeMatch to remove top image item")
	}

	// TTL expiry prunes items older than 10 minutes.
	now = now.Add(11 * time.Minute)
	if len(w.state().Items) != 0 {
		t.Fatalf("expected all entries expired after 11 minutes, got %+v", w.state().Items)
	}

	// Disabling stops OS listener and wipes state.
	w.recordEntry(mustEntry("ephemeral"))
	if len(w.state().Items) != 1 {
		t.Fatal("expected item before disable")
	}
	w.setEnabled(false)
	if !stopped {
		t.Error("expected disable to invoke off()")
	}
	if st := w.state(); st.Enabled || len(st.Items) != 0 {
		t.Fatalf("expected clean state on disable, got %+v", st)
	}
}
