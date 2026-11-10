// Package synclog keeps the record of recent syncs: when each ran, what it
// moved, and what it could not bring across. It is one JSON file, capped at
// MaxEntries, so the settings window can show what happened.
package synclog

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MaxEntries is how many syncs the log keeps.
const MaxEntries = 200

// Entry is one sync.
type Entry struct {
	Time       time.Time `json:"time"`
	Folder     string    `json:"folder"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	Uploaded   []string  `json:"uploaded,omitempty"`
	Downloaded []string  `json:"downloaded,omitempty"`
	Merged     []string  `json:"merged,omitempty"`
	Conflicts  []string  `json:"conflicts,omitempty"`
	Trashed    []string  `json:"trashed,omitempty"`
	Failed     []string  `json:"failed,omitempty"`
}

// Log is the sync log file. Its methods are safe for concurrent use.
type Log struct {
	mu   sync.Mutex
	path string
}

// Open returns the log at path. The file is created on the first Append.
func Open(path string) *Log { return &Log{path: path} }

// Append adds an entry, dropping the oldest once the log is full.
func (l *Log) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.read()
	if err != nil {
		return err
	}
	entries = append(entries, e)
	if len(entries) > MaxEntries {
		entries = entries[len(entries)-MaxEntries:]
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// Entries returns the entries, most recent first.
func (l *Log) Entries() ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.read()
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

func (l *Log) read() ([]Entry, error) {
	data, err := os.ReadFile(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
