package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-webdav"
)

// WebDAV is a folder on a WebDAV server (Nextcloud, Synology, Jianguoyun,
// Alist…). Its paths are relative to the folder's URL.
type WebDAV struct {
	client *webdav.Client
	root   string
}

// NewWebDAV connects to the folder at endpoint with basic authentication.
// transport may be nil.
func NewWebDAV(endpoint, user, password string, transport http.RoundTripper) (*WebDAV, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("storage: WebDAV address must start with http:// or https://")
	}
	httpClient := &http.Client{Timeout: 2 * time.Minute, Transport: transport}
	var hc webdav.HTTPClient = httpClient
	if user != "" {
		hc = webdav.HTTPClientWithBasicAuth(httpClient, user, password)
	}
	client, err := webdav.NewClient(hc, endpoint)
	if err != nil {
		return nil, err
	}
	root := strings.TrimSuffix(u.Path, "/")
	return &WebDAV{client: client, root: root}, nil
}

// relative turns a server path into a path under the folder.
func (w *WebDAV) relative(serverPath string) string {
	p, err := url.PathUnescape(serverPath)
	if err != nil {
		p = serverPath
	}
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimPrefix(p, w.root)
	return strings.TrimPrefix(p, "/")
}

// isStatus reports whether err is an HTTP error with the status code, which
// go-webdav's error texts start with.
func isStatus(err error, code int) bool {
	return err != nil && strings.HasPrefix(err.Error(), fmt.Sprintf("%d ", code))
}

func tagOfInfo(etag string, modTime time.Time, size int64) string {
	if etag != "" {
		return etag
	}
	return fmt.Sprintf("%d:%d", modTime.UnixNano(), size)
}

// List returns every file under the folder. One infinite-depth PROPFIND does
// it on most servers; a server that refuses it is walked one folder at a time.
func (w *WebDAV) List(ctx context.Context) ([]Entry, error) {
	if infos, err := w.client.ReadDir(ctx, "", true); err == nil {
		entries := []Entry{}
		for _, info := range infos {
			rel := w.relative(info.Path)
			if rel == "" || info.IsDir {
				continue
			}
			entries = append(entries, Entry{Path: rel, Size: info.Size, ModTime: info.ModTime, Tag: tagOfInfo(info.ETag, info.ModTime, info.Size)})
		}
		return entries, nil
	} else if isStatus(err, http.StatusTooManyRequests) || isStatus(err, http.StatusServiceUnavailable) {
		return nil, busy(err)
	}
	return w.listByLevel(ctx)
}

// listByLevel walks the folder one level at a time, for servers that refuse
// infinite-depth PROPFIND.
func (w *WebDAV) listByLevel(ctx context.Context) ([]Entry, error) {
	entries := []Entry{}
	queue := []string{""}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		infos, err := w.client.ReadDir(ctx, dir, false)
		if err != nil {
			if dir == "" {
				return nil, busy(err)
			}
			return nil, fmt.Errorf("storage: listing %s: %w", dir, busy(err))
		}
		for _, info := range infos {
			rel := w.relative(info.Path)
			if rel == "" || rel == strings.Trim(dir, "/") {
				continue
			}
			if info.IsDir {
				queue = append(queue, rel)
				continue
			}
			entries = append(entries, Entry{Path: rel, Size: info.Size, ModTime: info.ModTime, Tag: tagOfInfo(info.ETag, info.ModTime, info.Size)})
		}
	}
	return entries, nil
}

// Get reads a file.
func (w *WebDAV) Get(ctx context.Context, p string) ([]byte, error) {
	rc, err := w.client.Open(ctx, p)
	if isStatus(err, http.StatusNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, busy(err)
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Put writes a file, creating the folders on its way.
func (w *WebDAV) Put(ctx context.Context, p string, data []byte) (Entry, error) {
	if err := w.mkdirParents(ctx, p); err != nil {
		return Entry{}, err
	}
	wc, err := w.client.Create(ctx, p)
	if err != nil {
		return Entry{}, busy(err)
	}
	if _, err := io.Copy(wc, bytes.NewReader(data)); err != nil {
		_ = wc.Close()
		return Entry{}, err
	}
	if err := wc.Close(); err != nil {
		return Entry{}, err
	}
	info, err := w.client.Stat(ctx, p)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: p, Size: info.Size, ModTime: info.ModTime, Tag: tagOfInfo(info.ETag, info.ModTime, info.Size)}, nil
}

// Move renames a file over the destination.
func (w *WebDAV) Move(ctx context.Context, from, to string) error {
	if err := w.mkdirParents(ctx, to); err != nil {
		return err
	}
	return busy(w.client.Move(ctx, from, to, &webdav.MoveOptions{}))
}

// Close releases nothing: each request opens its own connection.
func (w *WebDAV) Close() error { return nil }

// mkdirParents creates each folder above p that is not there yet.
func (w *WebDAV) mkdirParents(ctx context.Context, p string) error {
	dir := path.Dir(p)
	if dir == "." || dir == "" {
		return nil
	}
	var made string
	for _, part := range strings.Split(dir, "/") {
		if made == "" {
			made = part
		} else {
			made = made + "/" + part
		}
		if info, err := w.client.Stat(ctx, made); err == nil && info.IsDir {
			continue
		}
		if err := w.client.Mkdir(ctx, made); err != nil && !isStatus(err, http.StatusMethodNotAllowed) {
			return err
		}
	}
	return nil
}
