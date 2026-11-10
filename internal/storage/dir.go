package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

// Dir is a folder on this machine, used as a remote. Its tag is the SHA-256
// of the content, so a change is never missed.
type Dir struct {
	Root string
}

func (d Dir) full(p string) string {
	return filepath.Join(d.Root, filepath.FromSlash(p))
}

func tagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// List walks the folder.
func (d Dir) List(ctx context.Context) ([]Entry, error) {
	entries := []Entry{}
	err := filepath.WalkDir(d.Root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if de.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		entries = append(entries, Entry{Path: filepath.ToSlash(rel), Size: info.Size(), ModTime: info.ModTime(), Tag: tagOf(data)})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return entries, nil
	}
	return entries, err
}

// Get reads a file.
func (d Dir) Get(ctx context.Context, p string) ([]byte, error) {
	data, err := os.ReadFile(d.full(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

// Put writes a file, replacing it atomically.
func (d Dir) Put(ctx context.Context, p string, data []byte) (Entry, error) {
	full := d.full(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Entry{}, err
	}
	tmp := full + ".tmp-put"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return Entry{}, err
	}
	return Entry{Path: path.Clean(p), Size: int64(len(data)), ModTime: time.Now(), Tag: tagOf(data)}, nil
}

// Move renames a file over the destination.
func (d Dir) Move(ctx context.Context, from, to string) error {
	dst := d.full(to)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(d.full(from), dst)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// Close does nothing: a folder holds no connection.
func (d Dir) Close() error { return nil }
