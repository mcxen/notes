package share

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Publication is where a page is published. It is kept on this computer only
// (it does not sync), so a page is never published at two addresses by two
// computers.
type Publication struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Parent    string    `json:"parent,omitempty"`
	Published time.Time `json:"published"`
}

// Store is the publications of every vault, by vault and page.
type Store struct {
	mu   sync.Mutex
	path string
}

// OpenStore reads and writes the file at path.
func OpenStore(path string) *Store { return &Store{path: path} }

type publications map[string]map[string]Publication

func (s *Store) load() (publications, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return publications{}, nil
	}
	if err != nil {
		return nil, err
	}
	var all publications
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	if all == nil {
		all = publications{}
	}
	return all, nil
}

func (s *Store) save(all publications) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Of is the publications of one vault, by page id.
func (s *Store) Of(vault string) (map[string]Publication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return nil, err
	}
	out := map[string]Publication{}
	for id, p := range all[vault] {
		out[id] = p
	}
	return out, nil
}

// Set records where a page of a vault is published.
func (s *Store) Set(vault, page string, p Publication) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	if all[vault] == nil {
		all[vault] = map[string]Publication{}
	}
	all[vault][page] = p
	return s.save(all)
}

// Forget removes a page's publication.
func (s *Store) Forget(vault, page string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	delete(all[vault], page)
	if len(all[vault]) == 0 {
		delete(all, vault)
	}
	return s.save(all)
}

// ForgetID removes every publication of the page that is published at id, in
// any vault: the worker no longer has it.
func (s *Store) ForgetID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	for vault, pages := range all {
		for page, p := range pages {
			if p.ID == id {
				delete(pages, page)
			}
		}
		if len(pages) == 0 {
			delete(all, vault)
		}
	}
	return s.save(all)
}
