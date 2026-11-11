// Update source with the tvv.tw GitHub mirror: manifests, archives and
// deltas are fetched through the mirror first and from GitHub when the
// mirror fails, times out or serves a file whose size or Ed25519 signature
// does not match the manifest. mygo verifies the signature again on install.
//
// The design follows Lumiwerk's update_mirror.go: mygo.Updater reads a GitHub
// release feed, and a source of our own puts the mirror in front of it.
package frontend

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
)

const (
	updateRepo      = "mcxen/notes"
	updatePublicKey = "xZ5864QHooA4ypEO11T2eny7aY7ZqsqxZ9a8FAI5nIY=" // = mygo.json updates.publicKey
	updateMirror    = "https://tvv.tw/"
	updateDownload  = "https://github.com/" + updateRepo + "/releases/download/"

	updateSourceAuto   = ""
	updateSourceGitHub = "github"
	updateSourceMirror = "tvv"
)

var updateSourceChoices = []string{updateSourceAuto, updateSourceGitHub, updateSourceMirror}

func updateSourceName(s string) string {
	switch s {
	case updateSourceGitHub:
		return "GitHub"
	case updateSourceMirror:
		return "tvv.tw mirror"
	}
	return "Automatic"
}

// applyUpdateSource points mygo's updater at the chosen source. Auto uses
// the mirror for Chinese locales only.
func applyUpdateSource(choice string) {
	useMirror := choice == updateSourceMirror ||
		choice == updateSourceAuto && strings.HasPrefix(strings.ToLower(mygo.App.Locale()), "zh")
	if !useMirror {
		mygo.Updater.SetSource(nil)
		return
	}
	mygo.Updater.SetSource(newMirrorSource())
}

// file is what the last manifest says about one downloadable file.
type mirrorFile struct {
	size      int64
	signature string
}

type mirrorSource struct {
	target string
	pub    ed25519.PublicKey
	mu     sync.Mutex
	files  map[string]mirrorFile
	// get fetches a URL; replaced by tests.
	get func(ctx context.Context, url string, timeout time.Duration) (io.ReadCloser, error)
}

func newMirrorSource() *mirrorSource {
	key, _ := base64.StdEncoding.DecodeString(updatePublicKey)
	target := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "darwin" {
		target = "darwin-universal"
	}
	return &mirrorSource{target: target, pub: ed25519.PublicKey(key), files: map[string]mirrorFile{}, get: updateHTTPGet}
}

func updateHTTPGet(ctx context.Context, url string, timeout time.Duration) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", appName+"/"+appVersion())
	resp, err := (&http.Client{Transport: netproxy.Transport()}).Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return &cancelBody{resp.Body, cancel}, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error { err := b.ReadCloser.Close(); b.cancel(); return err }

// mirrored is url through the mirror, or "" for URLs outside our releases.
func mirrored(url string) string {
	if !strings.HasPrefix(url, updateDownload) && !strings.HasPrefix(url, "https://api.github.com/repos/"+updateRepo+"/") {
		return ""
	}
	return updateMirror + url
}

// readSmall reads a small JSON document through the mirror, then GitHub.
func (s *mirrorSource) readSmall(ctx context.Context, url string) ([]byte, error) {
	var errs []error
	for _, u := range []string{mirrored(url), url} {
		body, err := s.get(ctx, u, 20*time.Second)
		if err == nil {
			var b []byte
			b, err = io.ReadAll(io.LimitReader(body, 1<<20))
			body.Close()
			if err == nil && json.Valid(b) {
				return b, nil
			}
			if err == nil {
				err = fmt.Errorf("%s: not JSON", u)
			}
		}
		log.Printf("notes: update: %v", err)
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

func (s *mirrorSource) Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error) {
	api := "https://api.github.com/repos/" + updateRepo + "/releases/latest"
	if prereleases {
		api = "https://api.github.com/repos/" + updateRepo + "/releases?per_page=1"
	}
	b, err := s.readSmall(ctx, api)
	if err != nil {
		return nil, err
	}
	var tag string
	var one struct {
		Tag string `json:"tag_name"`
	}
	var list []struct {
		Tag string `json:"tag_name"`
	}
	if json.Unmarshal(b, &one) == nil && one.Tag != "" {
		tag = one.Tag
	} else if json.Unmarshal(b, &list) == nil && len(list) > 0 {
		tag = list[0].Tag
	}
	if tag == "" || strings.ContainsAny(tag, "/?#") {
		return nil, errors.New("update: no published release")
	}
	b, err = s.readSmall(ctx, updateDownload+tag+"/update-"+s.target+".json")
	if err != nil {
		return nil, err
	}
	var m struct {
		URL       string `json:"url"`
		Size      int64  `json:"size"`
		Signature string `json:"signature"`
		Deltas    []struct {
			URL       string `json:"url"`
			Size      int64  `json:"size"`
			Signature string `json:"signature"`
		} `json:"deltas"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.files = map[string]mirrorFile{m.URL: {m.Size, m.Signature}}
	for _, d := range m.Deltas {
		s.files[d.URL] = mirrorFile{d.Size, d.Signature}
	}
	s.mu.Unlock()
	return io.NopCloser(bytes.NewReader(b)), nil
}

// Open downloads url through the mirror into a temporary file and checks it
// against the manifest; anything wrong falls back to GitHub.
func (s *mirrorSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	s.mu.Lock()
	want, known := s.files[url]
	s.mu.Unlock()
	if m := mirrored(url); known && m != "" {
		f, err := s.fetchVerified(ctx, m, want)
		if err == nil {
			return f, nil
		}
		log.Printf("notes: update: mirror failed, using GitHub: %v", err)
	}
	return s.get(ctx, url, 30*time.Minute)
}

func (s *mirrorSource) fetchVerified(ctx context.Context, url string, want mirrorFile) (io.ReadCloser, error) {
	body, err := s.get(ctx, url, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	f, err := os.CreateTemp("", "notes-update-*")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.ReadCloser, error) {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(body, want.size+1))
	if err != nil {
		return fail(err)
	}
	if n != want.size {
		return fail(fmt.Errorf("%s: %d bytes, want %d", url, n, want.size))
	}
	sig, err := base64.StdEncoding.DecodeString(want.signature)
	if err != nil || !ed25519.Verify(s.pub, sum.Sum(nil), sig) {
		return fail(fmt.Errorf("%s: bad signature", url))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return &tempFile{f}, nil
}

type tempFile struct{ *os.File }

func (t *tempFile) Close() error { err := t.File.Close(); os.Remove(t.File.Name()); return err }
