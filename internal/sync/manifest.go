// Package sync brings a local vault folder and a remote folder to the same
// files. It keeps a manifest of what both sides held at the last sync, which
// tells a change on one side from a change on the other. A page file changed
// on both sides is merged record by record and field by field; other files
// changed on both sides are kept twice.
package sync

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Entry is what a file was at the last sync.
type Entry struct {
	// Base is the SHA-256 of the file's content then.
	Base string `json:"base"`
	// Tag is the remote tag then (storage.Entry.Tag).
	Tag string `json:"tag"`
	// LocalSize and LocalMod are the local file's size and modification time
	// (unix nanoseconds) then, so an unchanged file is not read again.
	LocalSize int64 `json:"localSize"`
	LocalMod  int64 `json:"localMod"`
}

// Manifest is the state of a sync: one entry per file both sides hold.
type Manifest struct {
	Files map[string]Entry `json:"files"`
}

// LoadManifest reads the manifest at path. A missing file is an empty manifest.
func LoadManifest(path string) (*Manifest, error) {
	m := &Manifest{Files: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, err
	}
	if m.Files == nil {
		m.Files = map[string]Entry{}
	}
	return m, nil
}

// Save writes the manifest atomically.
func (m *Manifest) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
