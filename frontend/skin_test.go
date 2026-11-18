package frontend

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"notes/internal/settings"
)

func TestImportSkinImageCopiesIntoTheSkinsFolder(t *testing.T) {
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "settings.json")
	src := filepath.Join(dir, "Tony Stark.JPG")
	if err := os.WriteFile(src, []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err := importSkinImage(settingsFile, src)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.ValidSkinImage(name) {
		t.Fatalf("the name %q is not one the skins folder holds", name)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "skins", name)); string(got) != "pixels" {
		t.Fatalf("copy = %q", got)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("the original was touched")
	}
	if _, err := importSkinImage(settingsFile, filepath.Join(dir, "notes.txt")); err == nil {
		t.Fatal("a text file was taken as a picture")
	}
}

func TestServeSkinFilesServesOnlyThePictures(t *testing.T) {
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := importSkinImage(settingsFile, writeTemp(t, dir, "a.png"))
	if err != nil {
		t.Fatal(err)
	}
	h := serveSkinFiles(settingsFile)
	for path, want := range map[string]int{"/" + name: 200, "/../settings.json": 404, "/settings.json": 404, "/missing-1.png": 404} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
}

func TestSamePictureIsOneFileWithAThumbnail(t *testing.T) {
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "settings.json")
	pic := filepath.Join(dir, "p.png")
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for x := 0; x < 1600; x++ {
		img.Set(x, 10, color.RGBA{200, 30, 30, 255})
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pic, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := importSkinImage(settingsFile, pic)
	if err != nil {
		t.Fatal(err)
	}
	second, err := importSkinImage(settingsFile, pic)
	if err != nil || second != first {
		t.Fatalf("the same picture got two names: %q, %q (%v)", first, second, err)
	}
	f, err := os.Open(settings.ThumbFile(settingsFile, first))
	if err != nil {
		t.Fatalf("no thumbnail: %v", err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width != thumbWidth || cfg.Height != 360 {
		t.Fatalf("thumbnail %+v, %v", cfg, err)
	}
}

func TestPruneSkinImagesKeepsOnlyWhatTheSkinUses(t *testing.T) {
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "settings.json")
	keep, _ := importSkinImage(settingsFile, writeTemp(t, dir, "a.png"))
	drop, _ := importSkinImage(settingsFile, writeTemp(t, dir, "b.png"))
	pruneSkinImages(settingsFile, settings.Skin{History: []string{keep}})
	if !fileExists(filepath.Join(dir, "skins", keep)) || fileExists(filepath.Join(dir, "skins", drop)) {
		t.Fatal("the folder was not pruned to the pictures the history keeps")
	}
}

func writeTemp(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
