// Package storage is the remote side of a sync: a folder on a server, which
// the sync engine lists, reads and writes through one small interface. WebDAV
// and SMB implement it; Dir implements it over a local folder, for tests and
// for a folder the user keeps elsewhere.
package storage

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is what Get returns for a file that is not there.
var ErrNotFound = errors.New("storage: not found")

// Entry is a file on the remote side.
type Entry struct {
	// Path is the file's path relative to the remote folder, separated by
	// slashes.
	Path    string
	Size    int64
	ModTime time.Time
	// Tag changes whenever the content does: the server's ETag, or the
	// modification time and size where it has none.
	Tag string
}

// Remote is a folder on a server.
type Remote interface {
	// List returns every file under the folder, recursively.
	List(ctx context.Context) ([]Entry, error)
	// Get returns a file's bytes, or ErrNotFound.
	Get(ctx context.Context, path string) ([]byte, error)
	// Put replaces a file's bytes, creating the folders on its way.
	Put(ctx context.Context, path string, data []byte) (Entry, error)
	// Move renames a file, replacing whatever is at the destination.
	Move(ctx context.Context, from, to string) error
	Close() error
}
