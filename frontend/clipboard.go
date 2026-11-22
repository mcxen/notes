package frontend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ClipboardChanged broadcasts the unconsumed recent clipboard queue to open
// vault windows whenever the watched clipboard queue changes.
var ClipboardChanged = mygo.NewEvent[ClipboardState]("clipboard:changed")

const (
	clipboardMaxItems     = 5
	clipboardTTL          = 10 * time.Minute
	clipboardMaxText      = 2 << 20
	clipboardMaxImage     = 20 << 20
	clipboardPreviewRunes = 140
	clipboardSampleRunes  = 1500
	clipboardThumbMaxW    = 128
	clipboardThumbMaxH    = 64
)

// ClipboardPreview is the lightweight metadata sent over events for one recent
// clipboard entry. Heavy payloads (full HTML / full-resolution PNG) stay in Go
// memory until the user actually adopts the entry with Tab.
type ClipboardPreview struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`                   // "text" | "url" | "html" | "image"
	Preview      string `json:"preview"`                // Single-line display summary, or "W×H" for images
	Lines        int    `json:"lines,omitempty"`        // Line count when > 1
	Sample       string `json:"sample,omitempty"`       // Head of multi-line plain text for code-language hint
	ImagePreview string `json:"imagePreview,omitempty"` // Small data:image/png;base64,... thumbnail
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	CopiedAt     string `json:"copiedAt"` // RFC3339
}

// ClipboardState is the enabled flag and up to 5 unconsumed clipboard previews,
// newest first.
type ClipboardState struct {
	Enabled bool               `json:"enabled"`
	Items   []ClipboardPreview `json:"items"`
}

// ClipboardPayload is the full content returned once when the user adopts an
// item from the ghost preview.
type ClipboardPayload struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	Image     string `json:"image,omitempty"`
	ImageType string `json:"imageType,omitempty"`
	ImageName string `json:"imageName,omitempty"`
}

type clipboardEntry struct {
	preview  ClipboardPreview
	payload  ClipboardPayload
	hash     [32]byte
	copiedAt time.Time
}

type clipboardWatcher struct {
	mu                sync.Mutex
	enabled           bool
	off               func()
	seq               uint64
	items             []clipboardEntry
	suppressUntil     time.Time
	lastDismissedHash [32]byte
	lastDismissedAt   time.Time
	hasDismissedHash  bool

	now       func() time.Time
	subscribe func(func()) func()
	captureFn func(ignoreFocus bool)
	broadcast func(ClipboardState)
}

func newClipboardWatcher(broadcast func(ClipboardState)) *clipboardWatcher {
	w := &clipboardWatcher{
		now:       time.Now,
		subscribe: mygo.Clipboard.OnChanged,
		broadcast: broadcast,
	}
	w.captureFn = w.capture
	return w
}

func (w *clipboardWatcher) currentNow() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *clipboardWatcher) emit(st ClipboardState) {
	if w.broadcast != nil {
		w.broadcast(st)
	}
}

func (w *clipboardWatcher) pruneLocked(now time.Time) bool {
	if len(w.items) == 0 {
		return false
	}
	kept := w.items[:0]
	for _, entry := range w.items {
		if now.Sub(entry.copiedAt) <= clipboardTTL {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(w.items) {
		return false
	}
	clear(w.items[len(kept):])
	w.items = kept
	return true
}

func (w *clipboardWatcher) snapshotLocked() ClipboardState {
	previews := make([]ClipboardPreview, len(w.items))
	for i, entry := range w.items {
		previews[i] = entry.preview
	}
	return ClipboardState{
		Enabled: w.enabled,
		Items:   previews,
	}
}

func (w *clipboardWatcher) setEnabled(on bool) {
	w.mu.Lock()
	if on == w.enabled {
		w.mu.Unlock()
		return
	}
	if !on {
		off := w.off
		w.off = nil
		w.enabled = false
		w.items = nil
		w.hasDismissedHash = false
		w.lastDismissedHash = [32]byte{}
		w.lastDismissedAt = time.Time{}
		st := w.snapshotLocked()
		w.mu.Unlock()
		if off != nil {
			off()
		}
		w.emit(st)
		return
	}
	w.enabled = true
	sub := w.subscribe
	capFn := w.captureFn
	st := w.snapshotLocked()
	w.mu.Unlock()

	if sub != nil {
		off := sub(func() {
			if capFn != nil {
				capFn(false)
			}
		})
		w.mu.Lock()
		if !w.enabled {
			w.mu.Unlock()
			if off != nil {
				off()
			}
			return
		}
		w.off = off
		w.mu.Unlock()
	}
	w.emit(st)
	if capFn != nil {
		capFn(true)
	}
}

func (w *clipboardWatcher) stop() {
	w.mu.Lock()
	off := w.off
	w.off = nil
	w.enabled = false
	w.items = nil
	w.hasDismissedHash = false
	w.lastDismissedHash = [32]byte{}
	w.lastDismissedAt = time.Time{}
	w.mu.Unlock()
	if off != nil {
		off()
	}
}

func (w *clipboardWatcher) suppressFor(d time.Duration) {
	if w == nil {
		return
	}
	w.mu.Lock()
	until := w.currentNow().Add(d)
	if until.After(w.suppressUntil) {
		w.suppressUntil = until
	}
	w.mu.Unlock()
}

func (w *clipboardWatcher) state() ClipboardState {
	w.mu.Lock()
	w.pruneLocked(w.currentNow())
	st := w.snapshotLocked()
	w.mu.Unlock()
	return st
}

func (w *clipboardWatcher) pruneAndNotify(win *mygo.Window) {
	if win == nil {
		return
	}
	w.mu.Lock()
	w.pruneLocked(w.currentNow())
	st := w.snapshotLocked()
	w.mu.Unlock()
	_ = ClipboardChanged.Emit(win, st)
}

func (w *clipboardWatcher) recordEntry(entry clipboardEntry) {
	w.mu.Lock()
	if !w.enabled {
		w.mu.Unlock()
		return
	}
	w.pruneLocked(entry.copiedAt)
	if w.hasDismissedHash && entry.hash == w.lastDismissedHash && entry.copiedAt.Sub(w.lastDismissedAt) < 2*time.Second {
		w.mu.Unlock()
		return
	}
	if len(w.items) > 0 && w.items[0].hash == entry.hash {
		w.mu.Unlock()
		return
	}
	filtered := make([]clipboardEntry, 0, len(w.items))
	for _, existing := range w.items {
		if existing.hash != entry.hash {
			filtered = append(filtered, existing)
		}
	}
	w.seq++
	id := fmt.Sprintf("clip-%d", w.seq)
	entry.preview.ID = id
	entry.payload.ID = id

	next := make([]clipboardEntry, 0, min(len(filtered)+1, clipboardMaxItems))
	next = append(next, entry)
	for i := 0; i < len(filtered) && len(next) < clipboardMaxItems; i++ {
		next = append(next, filtered[i])
	}
	w.items = next
	w.hasDismissedHash = false
	w.lastDismissedHash = [32]byte{}
	w.lastDismissedAt = time.Time{}
	st := w.snapshotLocked()
	w.mu.Unlock()
	w.emit(st)
}

func (w *clipboardWatcher) take(id string) (ClipboardPayload, bool) {
	w.mu.Lock()
	now := w.currentNow()
	w.pruneLocked(now)
	idx := slices.IndexFunc(w.items, func(e clipboardEntry) bool { return e.preview.ID == id })
	if idx < 0 {
		w.mu.Unlock()
		return ClipboardPayload{}, false
	}
	entry := w.items[idx]
	if idx == 0 {
		w.lastDismissedHash = entry.hash
		w.lastDismissedAt = now
		w.hasDismissedHash = true
	}
	w.items = append(w.items[:idx], w.items[idx+1:]...)
	st := w.snapshotLocked()
	w.mu.Unlock()
	w.emit(st)
	return entry.payload, true
}

func (w *clipboardWatcher) dismiss(id string) {
	w.mu.Lock()
	now := w.currentNow()
	w.pruneLocked(now)
	idx := slices.IndexFunc(w.items, func(e clipboardEntry) bool { return e.preview.ID == id })
	if idx < 0 {
		w.mu.Unlock()
		return
	}
	if idx == 0 {
		w.lastDismissedHash = w.items[idx].hash
		w.lastDismissedAt = now
		w.hasDismissedHash = true
	}
	w.items = append(w.items[:idx], w.items[idx+1:]...)
	st := w.snapshotLocked()
	w.mu.Unlock()
	w.emit(st)
}

func (w *clipboardWatcher) consumeMatch(text string) {
	w.mu.Lock()
	now := w.currentNow()
	pruned := w.pruneLocked(now)
	removed := false
	norm := strings.TrimSpace(normalizeNewlines(text))
	if norm == "" {
		if len(w.items) > 0 && w.items[0].preview.Kind == "image" {
			w.lastDismissedHash = w.items[0].hash
			w.lastDismissedAt = now
			w.hasDismissedHash = true
			w.items = w.items[1:]
			removed = true
		}
	} else {
		for i, entry := range w.items {
			if entry.preview.Kind == "image" {
				continue
			}
			if strings.TrimSpace(normalizeNewlines(entry.payload.Text)) == norm {
				if i == 0 {
					w.lastDismissedHash = entry.hash
					w.lastDismissedAt = now
					w.hasDismissedHash = true
				}
				w.items = append(w.items[:i], w.items[i+1:]...)
				removed = true
				break
			}
		}
	}
	var st ClipboardState
	if pruned || removed {
		st = w.snapshotLocked()
	}
	w.mu.Unlock()
	if pruned || removed {
		w.emit(st)
	}
}

// capture inspects the OS clipboard on the MyGo main thread and offloads
// hashing / thumbnail scaling to a background goroutine.
func (w *clipboardWatcher) capture(ignoreFocus bool) {
	w.mu.Lock()
	enabled := w.enabled
	suppressed := w.currentNow().Before(w.suppressUntil)
	w.mu.Unlock()
	if !enabled || suppressed {
		return
	}

	rawFormats := mygo.Clipboard.AvailableFormats()
	if hasNotesInternalFormat(rawFormats) {
		return
	}

	formats := mygo.Clipboard.Formats()
	hasFmt := func(target transfer.Format) bool { return slices.Contains(formats, target) }
	if hasFmt(transfer.FileList) && !hasFmt(transfer.HTML) {
		return
	}

	hasImg := hasFmt(transfer.PNG)
	hasTxt := hasFmt(transfer.Text) || hasFmt(transfer.Markdown) || len(formats) == 0
	hasHTML := hasFmt(transfer.HTML)
	focused := !ignoreFocus && mygo.FocusedWindow() != nil
	if focused && !hasImg {
		return
	}

	var text, html string
	if hasTxt {
		text = mygo.Clipboard.ReadText()
	}
	if hasHTML {
		html = mygo.Clipboard.ReadHTML()
	}
	if focused && strings.TrimSpace(text) != "" && !isImageOnlyHTML(html) {
		return
	}

	var imgBytes []byte
	if hasImg && (strings.TrimSpace(text) == "" || isImageOnlyHTML(html)) {
		imgBytes = mygo.Clipboard.ReadImage()
	}
	if strings.TrimSpace(text) == "" && strings.TrimSpace(html) == "" && len(imgBytes) == 0 {
		return
	}

	now := w.currentNow()
	goSafe("clipboard capture", func() {
		entry, ok := classifyAndBuildEntry(text, html, imgBytes, now)
		if !ok {
			return
		}
		w.recordEntry(entry)
	})
}

func hasNotesInternalFormat(rawFormats []string) bool {
	for _, f := range rawFormats {
		if strings.Contains(strings.ToLower(f), "application/x-notes-blocks") {
			return true
		}
	}
	return false
}

var (
	reHTMLTag = regexp.MustCompile(`(?is)<(h[1-6]|ul|ol|li|table|tr|td|th|a|img|strong|b|em|i|pre|code|blockquote|p)\b`)
	reAnyTag  = regexp.MustCompile(`(?is)<[^>]+>`)
	reImgTag  = regexp.MustCompile(`(?is)<img\b`)
	reSpace   = regexp.MustCompile(`\s+`)
)

func hasRichHTML(html string) bool {
	return reHTMLTag.MatchString(html)
}

func isImageOnlyHTML(html string) bool {
	if !reImgTag.MatchString(html) {
		return false
	}
	stripped := reAnyTag.ReplaceAllString(html, "")
	return strings.TrimSpace(stripped) == ""
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func isWebURL(line string) bool {
	if line == "" || strings.ContainsAny(line, " \t\r\n") {
		return false
	}
	if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
		return false
	}
	u, err := url.ParseRequestURI(line)
	return err == nil && u.Host != ""
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var out strings.Builder
	count := 0
	for _, r := range s {
		if count >= max-1 {
			out.WriteRune('…')
			break
		}
		out.WriteRune(r)
		count++
	}
	return out.String()
}

func previewFirstLine(norm string) string {
	for _, line := range strings.Split(norm, "\n") {
		collapsed := strings.TrimSpace(reSpace.ReplaceAllString(line, " "))
		if collapsed != "" {
			return truncateRunes(collapsed, clipboardPreviewRunes)
		}
	}
	return ""
}

func makeImageThumbnail(raw []byte) (dataURL string, width, height int, ok bool) {
	if len(raw) == 0 || len(raw) > clipboardMaxImage {
		return "", 0, 0, false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", 0, 0, false
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return "", 0, 0, false
	}
	tw, th := w, h
	if tw > clipboardThumbMaxW || th > clipboardThumbMaxH {
		if float64(tw)/float64(clipboardThumbMaxW) > float64(th)/float64(clipboardThumbMaxH) {
			th = max(1, (h*clipboardThumbMaxW+w/2)/w)
			tw = clipboardThumbMaxW
		} else {
			tw = max(1, (w*clipboardThumbMaxH+h/2)/h)
			th = clipboardThumbMaxH
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", w, h, true
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), w, h, true
}

func classifyAndBuildEntry(text, html string, imgBytes []byte, now time.Time) (clipboardEntry, bool) {
	if strings.Contains(html, "data-notes-blocks") {
		return clipboardEntry{}, false
	}
	copiedAt := now.UTC().Format(time.RFC3339)

	if len(imgBytes) > 0 && len(imgBytes) <= clipboardMaxImage {
		thumb, w, h, ok := makeImageThumbnail(imgBytes)
		if !ok {
			return clipboardEntry{}, false
		}
		hsum := sha256.New()
		hsum.Write([]byte("image\x00"))
		hsum.Write(imgBytes)
		var digest [32]byte
		copy(digest[:], hsum.Sum(nil))
		return clipboardEntry{
			hash:     digest,
			copiedAt: now,
			preview: ClipboardPreview{
				Kind:         "image",
				Preview:      fmt.Sprintf("%d×%d", w, h),
				ImagePreview: thumb,
				Width:        w,
				Height:       h,
				CopiedAt:     copiedAt,
			},
			payload: ClipboardPayload{
				Kind:      "image",
				Image:     base64.StdEncoding.EncodeToString(imgBytes),
				ImageType: "image/png",
				ImageName: fmt.Sprintf("clipboard-%s.png", now.Format("20060102-150405")),
			},
		}, true
	}

	if len(text) > clipboardMaxText {
		return clipboardEntry{}, false
	}
	norm := normalizeNewlines(text)
	trimmed := strings.TrimSpace(norm)
	if trimmed == "" {
		return clipboardEntry{}, false
	}

	trimmedNoTrail := strings.TrimRight(norm, "\n")
	lines := strings.Count(trimmedNoTrail, "\n") + 1
	preview := previewFirstLine(norm)
	if preview == "" {
		return clipboardEntry{}, false
	}

	kind := "text"
	storedHTML := ""
	if lines == 1 && isWebURL(trimmed) {
		kind = "url"
		norm = trimmed
	} else if hasRichHTML(html) {
		kind = "html"
		storedHTML = html
	}

	sample := ""
	if kind == "text" && lines >= 2 {
		sample = truncateRunes(norm, clipboardSampleRunes)
	}

	hsum := sha256.New()
	hsum.Write([]byte(kind + "\x00"))
	hsum.Write([]byte(norm))
	if storedHTML != "" {
		hsum.Write([]byte("\x00"))
		hsum.Write([]byte(storedHTML))
	}
	var digest [32]byte
	copy(digest[:], hsum.Sum(nil))

	lineCount := 0
	if lines > 1 {
		lineCount = lines
	}

	return clipboardEntry{
		hash:     digest,
		copiedAt: now,
		preview: ClipboardPreview{
			Kind:     kind,
			Preview:  preview,
			Lines:    lineCount,
			Sample:   sample,
			CopiedAt: copiedAt,
		},
		payload: ClipboardPayload{
			Kind: kind,
			Text: norm,
			HTML: storedHTML,
		},
	}, true
}

// ClipboardState returns whether clipboard watching is enabled and the recent
// unconsumed clipboard previews.
func (n *Notebooks) ClipboardState(ctx context.Context) (ClipboardState, error) {
	if n.clipboard == nil {
		return ClipboardState{Items: []ClipboardPreview{}}, nil
	}
	return n.clipboard.state(), nil
}

// TakeClipboardItem returns the full payload of a recent clipboard item and
// marks it consumed across open vault windows.
func (n *Notebooks) TakeClipboardItem(ctx context.Context, id string) (ClipboardPayload, error) {
	if n.clipboard == nil {
		return ClipboardPayload{}, nil
	}
	payload, _ := n.clipboard.take(id)
	return payload, nil
}

// DismissClipboardItem removes an entry from the ghost preview queue across
// open vault windows.
func (n *Notebooks) DismissClipboardItem(ctx context.Context, id string) error {
	if n.clipboard != nil {
		n.clipboard.dismiss(id)
	}
	return nil
}

// ConsumeClipboardMatch removes a matching item when the user pastes it
// manually with Cmd/Ctrl-V.
func (n *Notebooks) ConsumeClipboardMatch(ctx context.Context, text string) error {
	if n.clipboard != nil {
		n.clipboard.consumeMatch(text)
	}
	return nil
}
