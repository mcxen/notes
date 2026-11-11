package frontend

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"notes/internal/vault"
)

// A new vault is a folder made in a place and under a name the person picks:
// the place starts as Documents, the name as "My Notes" (or the next free one
// there), and a vault made with neither touched is one press away.

// defaultBaseName is the name a new vault is offered first.
func defaultBaseName() string { return tr("My Notes") }

// defaultLocation is where a new vault goes unless another place is chosen: the
// Documents folder, else the home folder.
func defaultLocation() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	if docs := filepath.Join(home, "Documents"); isDirectory(docs) {
		return docs
	}
	return home
}

// expandHome reads a path a person typed: "~" and "~/…" are their home folder.
func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// uniqueName is base, or base with the first number from 2 that is not yet a
// file or folder in location.
func uniqueName(location, base string) string {
	name := base
	for n := 2; n < 1000; n++ {
		if _, err := os.Lstat(filepath.Join(location, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s %d", base, n)
	}
	return name
}

// vaultFolderName checks a name for a folder: one name, not a path.
func vaultFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New(tr("Enter a name for the vault."))
	case name == "." || name == ".." || strings.ContainsAny(name, `/\:`) || strings.ContainsRune(name, 0) || len(name) > 200:
		return "", errors.New(tr("A vault's name must be a single name, without slashes or colons."))
	}
	return name, nil
}

// createVaultFolder makes the folder of a new vault, location/name, and returns
// its path. The location must be a folder already, and nothing may be at the
// path: a new vault is never made out of a folder that has something in it.
func createVaultFolder(location, name string) (string, error) {
	location = expandHome(location)
	if !isDirectory(location) {
		return "", errors.New(tr("The location is not a folder that exists. Choose another."))
	}
	name, err := vaultFolderName(name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(location, name)
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%s", tr("There is already something named “%s” there. Choose another name.", name))
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		return "", fmt.Errorf("%s", tr("The folder could not be made: %v", err))
	}
	return path, nil
}

// newID makes an id as the pages make theirs: a prefix and 16 hex digits.
func newID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%s_%016x", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

// seedWelcomePage gives a vault just made a first page, so that it opens on a
// page to write on rather than on an empty window.
func seedWelcomePage(folder string) error {
	v, err := vault.Open(folder)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	page := newID("page")
	put := func(c vault.Collection, id string, value vault.Record) error {
		_, err := v.Write(vault.Write{Action: "put", Collection: c, ID: id, Value: value})
		return err
	}
	err = put(vault.Pages, page, vault.Record{
		"id": page, "kind": "document", "icon": "",
		"title_content": []any{vault.Record{"text": tr("Welcome")}},
		"created_at":    now, "updated_at": now, "created_by": "Local notebook", "updated_by": "Local notebook",
	})
	for i, text := range []string{
		tr("This is your first page. Write here, or press / for headings, lists and more."),
		tr("Press Command-K to search your notes, and use the + in the sidebar for a new page."),
	} {
		if err != nil {
			break
		}
		block := newID("block")
		err = put(vault.Blocks, vault.BlockRecordID(page, block), vault.Record{
			"id": block, "kind": "text", "order": float64(1024 * (i + 1)), "content": []any{vault.Record{"text": text}}, "page": page,
		})
	}
	if closeErr := v.Close(); err == nil {
		err = closeErr
	}
	return err
}
