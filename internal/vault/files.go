package vault

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Attachments are kept as they are, in the attachments folder. A record names
// one by a path under files/ (web/src/core/files.ts), which maps to the same
// path under attachments/ on disk.

var attachmentPath = regexp.MustCompile(`^files/[A-Za-z0-9._\-/]{1,300}$`)

// attachmentRel maps a record's file path to its place under the vault. A
// path may not leave the attachments folder.
func attachmentRel(path string) (string, error) {
	if !attachmentPath.MatchString(path) {
		return "", errors.New("vault: invalid file path " + path)
	}
	rest := strings.TrimPrefix(path, "files/")
	for _, segment := range strings.Split(rest, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("vault: invalid file path " + path)
		}
	}
	return attachmentsDir + "/" + rest, nil
}

// WriteFile stores the bytes of a file at a record's path. The bytes reach
// the disk at once: attachments are not debounced.
func (v *Vault) WriteFile(path string, data []byte) error {
	rel, err := attachmentRel(path)
	if err != nil {
		return err
	}
	v.mu.Lock()
	closed := v.closed
	v.mu.Unlock()
	if closed {
		return errors.New("vault: the vault is closed")
	}
	return writeAtomic(filepath.Join(v.root, filepath.FromSlash(rel)), data)
}

// ReadFile returns a file's bytes. A missing file is fs.ErrNotExist.
func (v *Vault) ReadFile(path string) ([]byte, error) {
	full, err := v.AttachmentPath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// AttachmentPath is the file on disk a record's path names.
func (v *Vault) AttachmentPath(path string) (string, error) {
	rel, err := attachmentRel(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(v.root, filepath.FromSlash(rel)), nil
}

// AttachmentsRoot is the folder that holds the attachments, for serving.
func (v *Vault) AttachmentsRoot() string {
	return filepath.Join(v.root, attachmentsDir)
}
