package frontend

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"notes/internal/settings"
)

// LookChanged carries the new look to a vault window with no reload.
var LookChanged = mygo.NewEvent[Look]("look:changed")

// SkinLayerView is one side's background; a picture is named by its file.
type SkinLayerView struct {
	Kind       string `json:"kind"`
	Image      string `json:"image"`
	From       string `json:"from"`
	Via        string `json:"via"`
	To         string `json:"to"`
	Angle      int    `json:"angle"`
	Blur       int    `json:"blur"`
	Veil       int    `json:"veil"`
	Brightness int    `json:"brightness"`
	Zoom       int    `json:"zoom"`
	OffsetX    int    `json:"offsetX"`
	OffsetY    int    `json:"offsetY"`
}

// SkinView is the wallpaper a vault window draws, every value within limits.
type SkinView struct {
	Sidebar  SkinLayerView `json:"sidebar"`
	Page     SkinLayerView `json:"page"`
	Span     bool          `json:"span"`
	SpanVeil int           `json:"spanVeil"`
	Seam     int           `json:"seam"`
	SeamBlur int           `json:"seamBlur"`
}

func layerView(l settings.SkinLayer) SkinLayerView {
	l = l.Clean()
	return SkinLayerView{
		Kind: l.Kind, Image: l.Image, From: l.From, Via: l.Via, To: l.To, Angle: l.Angle,
		Blur: l.Blur, Veil: l.Veil, Brightness: l.Brightness, Zoom: l.Zoom, OffsetX: l.OffsetX, OffsetY: l.OffsetY,
	}
}

// skinView is the cleaned skin; a picture gone from disk is not drawn.
func skinView(st settings.Settings) SkinView {
	sk := st.Skin.Clean()
	for _, l := range []*settings.SkinLayer{&sk.Sidebar, &sk.Page} {
		if l.Kind == "image" {
			if f := settings.SkinFile(settingsPath(), l.Image); f == "" || !fileExists(f) {
				*l = settings.SkinLayer{}
			}
		}
	}
	return SkinView{
		Sidebar: layerView(sk.Sidebar), Page: layerView(sk.Page),
		Span: sk.Span && sk.Sidebar.Kind == "image", SpanVeil: sk.SpanVeil, Seam: sk.Seam, SeamBlur: sk.SeamBlur,
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// serveSkinFiles answers skin://localhost/<file>, only for names the app wrote.
func serveSkinFiles(settingsFile string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		file := settings.SkinFile(settingsFile, name)
		if file == "" {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		// The name changes with the content, so it can be cached for good.
		w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		http.ServeContent(w, r, info.Name(), modTimeOr(info.ModTime()), f)
	})
}

const maxSkinImage = 80 << 20

var skinExtensions = map[string]string{
	".jpg": "jpg", ".jpeg": "jpg", ".png": "png", ".webp": "webp", ".gif": "gif", ".avif": "avif", ".bmp": "bmp",
}

// importSkinImage copies a picture into the skins folder under a name made
// from its content (the same picture is one file) and makes a small copy.
func importSkinImage(settingsFile, source string) (string, error) {
	ext, ok := skinExtensions[strings.ToLower(filepath.Ext(source))]
	if !ok {
		return "", errors.New(tr("Choose a picture: JPG, PNG, WebP, GIF, AVIF or BMP."))
	}
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New(tr("Choose a picture file."))
	}
	if info.Size() > maxSkinImage {
		return "", errors.New(tr("The picture is larger than 80 MB."))
	}
	dir := settings.SkinsDir(settingsFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".import-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, sum), in); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	name := fmt.Sprintf("img-%x.%s", sum.Sum(nil)[:8], ext)
	target := filepath.Join(dir, name)
	if !fileExists(target) {
		if err := os.Rename(tmp.Name(), target); err != nil {
			return "", err
		}
	}
	if !fileExists(settings.ThumbFile(settingsFile, name)) {
		// A picture that cannot be reduced (AVIF) is shown without a small copy.
		_ = makeThumb(target, settings.ThumbFile(settingsFile, name))
	}
	return name, nil
}

// thumbWidth is big enough for the hover preview.
const thumbWidth = 640

func makeThumb(source, target string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return errors.New("empty picture")
	}
	if w > thumbWidth {
		h = max(1, h*thumbWidth/w)
		w = thumbWidth
	}
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(small, small.Bounds(), img, b, xdraw.Over, nil)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".thumb-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := jpeg.Encode(tmp, small, &jpeg.Options{Quality: 82}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// pruneSkinImages deletes pictures the skin neither uses nor keeps.
func pruneSkinImages(settingsFile string, sk settings.Skin) {
	keep := map[string]bool{}
	for _, name := range sk.Images() {
		keep[name] = true
	}
	dir := settings.SkinsDir(settingsFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && !keep[e.Name()] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	thumbs, err := os.ReadDir(filepath.Join(dir, "thumbs"))
	if err != nil {
		return
	}
	wanted := map[string]bool{}
	for name := range keep {
		wanted[filepath.Base(settings.ThumbFile(settingsFile, name))] = true
	}
	for _, e := range thumbs {
		if !e.IsDir() && !wanted[e.Name()] {
			_ = os.Remove(filepath.Join(dir, "thumbs", e.Name()))
		}
	}
}

// applySkin pushes the current look to every open vault window.
func (a *appActions) applySkin() {
	st, err := settings.Load(a.settings)
	if err != nil {
		return
	}
	look := lookOf(st)
	for _, w := range a.notebooks.windows() {
		_ = LookChanged.Emit(w, look)
	}
}
