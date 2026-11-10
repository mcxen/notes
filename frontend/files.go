package frontend

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// serveVaultFiles answers vault://localhost/<vault id>/files/<path>: an
// attachment of the vault the address names. The vault's id is in the path,
// not the host, because WebView2 serves a scheme at
// http://vault.localhost/ and takes only DNS names as hosts, which the
// underscore of an id is not. Only regular files under the vault's
// attachments folder are served, with range requests, so a picture or a video
// streams from the disk instead of being read into memory.
func serveVaultFiles(n *Notebooks) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, rest, _ := strings.Cut(strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/"), "/")
		s := n.byVaultID(id)
		if s == nil {
			http.NotFound(w, r)
			return
		}
		rel, ok := strings.CutPrefix(rest, "files/")
		if !ok || rel == "" || strings.Contains(rel, "..") {
			http.NotFound(w, r)
			return
		}
		fsys := os.DirFS(s.vault.AttachmentsRoot())
		file, err := fsys.Open(rel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		content, ok := file.(io.ReadSeeker)
		if !ok {
			http.Error(w, "attachment cannot be read", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, info.Name(), modTimeOr(info.ModTime()), content)
	})
}

func modTimeOr(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0)
	}
	return t
}
