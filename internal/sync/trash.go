package sync

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// TrashItem is a file a sync moved into a vault's .trash folder: where it was,
// and the day it was moved.
type TrashItem struct {
	// Path is the file's path in the vault, as it was before it was moved.
	Path string `json:"path"`
	// Trashed is the file's path under the vault, inside .trash.
	Trashed string `json:"trashed"`
	// Day is the folder it was moved to, YYYY-MM-DD.
	Day string `json:"day"`
}

// ListTrash returns what is in the vault's .trash, most recent day first.
func ListTrash(root string) ([]TrashItem, error) {
	items := []TrashItem{}
	base := filepath.Join(root, trashDir)
	err := filepath.WalkDir(base, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 3)
		if len(parts) < 3 {
			return nil
		}
		items = append(items, TrashItem{Path: parts[2], Trashed: filepath.ToSlash(rel), Day: parts[1]})
		return nil
	})
	if os.IsNotExist(err) {
		return items, nil
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Day != items[j].Day {
			return items[i].Day > items[j].Day
		}
		return items[i].Path < items[j].Path
	})
	return items, err
}

// RestoreTrash moves a trashed file back to where it was. When something is
// already there, the file goes back under a new name, so nothing is overwritten.
// It returns the path the file was restored to.
func RestoreTrash(root, trashed string) (string, error) {
	trashed = path.Clean(filepath.ToSlash(trashed))
	parts := strings.SplitN(trashed, "/", 3)
	if len(parts) < 3 || parts[0] != trashDir || parts[2] == "" || strings.Contains(parts[2], "..") {
		return "", fmt.Errorf("sync: %s is not in the trash", trashed)
	}
	target := parts[2]
	dst := filepath.Join(root, filepath.FromSlash(target))
	if _, err := os.Stat(dst); err == nil {
		dir, file := path.Split(target)
		ext := path.Ext(file)
		target = dir + fmt.Sprintf("%s (恢复 %s)%s", strings.TrimSuffix(file, ext), parts[1], ext)
		dst = filepath.Join(root, filepath.FromSlash(target))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(root, filepath.FromSlash(trashed)), dst); err != nil {
		return "", err
	}
	return target, nil
}
