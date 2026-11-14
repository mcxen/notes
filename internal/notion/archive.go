package notion

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Limits keep a zip that is not what it says from filling the disk: Notion's
// exports are large, but never a thousand times their size.
type Limits struct {
	// MaxEntries is how many files one export may hold.
	MaxEntries int
	// MaxFile is the largest file, and MaxTotal all of them, once unpacked.
	MaxFile, MaxTotal int64
	// MaxDepth is how deep zips may lie in zips, and MaxNested how many of
	// them are opened.
	MaxDepth, MaxNested int
}

// DefaultLimits are what an export may take.
var DefaultLimits = Limits{MaxEntries: 50_000, MaxFile: 1 << 30, MaxTotal: 8 << 30, MaxDepth: 2, MaxNested: 64}

// Export is a Notion export unpacked in a folder.
type Export struct {
	// Files are the paths of the files, relative to the export's own root:
	// slashes, composed Unicode, without Notion's wrapping folder.
	Files []string
	// Warnings say what was left out or changed on the way.
	Warnings []string
	// disk is where each file of Files is.
	disk map[string]string
}

// Open is a file of the export, by its path in Files.
func (e *Export) Open(rel string) (*os.File, error) {
	return os.Open(e.full(rel))
}

// Read is a file's bytes.
func (e *Export) Read(rel string) ([]byte, error) {
	return os.ReadFile(e.full(rel))
}

// Size is a file's size.
func (e *Export) Size(rel string) int64 {
	info, err := os.Stat(e.full(rel))
	if err != nil {
		return 0
	}
	return info.Size()
}

func (e *Export) full(rel string) string { return e.disk[rel] }

// Unpack opens the export at zipPath and unpacks it into dir, which must be an
// empty folder of the caller's: the caller removes it when it is done.
func Unpack(zipPath, dir string, limits Limits) (*Export, error) {
	u := &unpacker{dir: dir, limits: limits, seen: map[string]string{}}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("notion: this is not a zip file: %w", err)
	}
	defer zr.Close()
	if err := u.unpack(&zr.Reader, 0); err != nil {
		return nil, err
	}
	e := &Export{Warnings: u.warnings, disk: u.seen}
	for p := range u.seen {
		e.Files = append(e.Files, p)
	}
	sort.Strings(e.Files)
	e.stripWrapper()
	if len(e.Files) == 0 {
		return nil, errors.New("notion: the zip holds no files")
	}
	return e, nil
}

type unpacker struct {
	dir      string
	limits   Limits
	seen     map[string]string // path in the export -> file on disk
	total    int64
	nested   int
	warnings []string
}

var (
	// A zip inside the export that Notion made: large exports come in parts.
	exportPart = regexp.MustCompile(`(?i)(^|/)export[-_ ][^/]*\.zip$`)
	wrapper    = regexp.MustCompile(`(?i)^(?:export(?:block)?[-_ ]?[^/]*|notion(?:[-_ ]?export)?|private(?:[-_ ]&[-_ ]shared)?|workspace|my[-_ ]workspace|私人与共享)$`)
)

func (u *unpacker) unpack(zr *zip.Reader, depth int) error {
	onlyZips := true
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && !strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			onlyZips = false
		}
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, ok := entryName(f.Name)
		if !ok {
			u.warn("a file with an unsafe name was left out: " + f.Name)
			continue
		}
		if isSystemFile(name) {
			continue
		}
		if len(u.seen)+1 > u.limits.MaxEntries {
			return fmt.Errorf("notion: the export holds more than %d files", u.limits.MaxEntries)
		}
		if int64(f.UncompressedSize64) > u.limits.MaxFile {
			return fmt.Errorf("notion: %s is over the size limit", name)
		}
		isZip := strings.HasSuffix(strings.ToLower(name), ".zip")
		if isZip && depth < u.limits.MaxDepth && (exportPart.MatchString(name) || onlyZips) {
			if u.nested++; u.nested > u.limits.MaxNested {
				return fmt.Errorf("notion: the export holds more than %d zips", u.limits.MaxNested)
			}
			if err := u.unpackNested(f, depth+1); err != nil {
				return err
			}
			continue
		}
		if _, dup := u.seen[name]; dup {
			u.warn("a path appears twice, the first file is kept: " + name)
			continue
		}
		target := filepath.Join(u.dir, fmt.Sprintf("f%06d%s", len(u.seen), filepath.Ext(name)))
		n, err := u.copy(f, target)
		if err != nil {
			return err
		}
		if n == 0 {
			u.warn("an empty file: " + name)
		}
		u.seen[name] = target
	}
	return nil
}

// unpackNested opens a zip that lies in the zip being read.
func (u *unpacker) unpackNested(f *zip.File, depth int) error {
	tmp := filepath.Join(u.dir, fmt.Sprintf("nested%d.zip", u.nested))
	defer os.Remove(tmp)
	if _, err := u.copy(f, tmp); err != nil {
		return err
	}
	// The nested zip is not part of what the export holds.
	u.total -= int64(f.UncompressedSize64)
	zr, err := zip.OpenReader(tmp)
	if err != nil {
		u.warn("a zip inside the export could not be read: " + f.Name)
		return nil
	}
	defer zr.Close()
	return u.unpack(&zr.Reader, depth)
}

// copy unpacks one file, stopping at the limits however large the zip says it
// is.
func (u *unpacker) copy(f *zip.File, target string) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, u.limits.MaxFile+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if n > u.limits.MaxFile {
		return n, fmt.Errorf("notion: %s is over the size limit", f.Name)
	}
	if u.total += n; u.total > u.limits.MaxTotal {
		return n, errors.New("notion: the export unpacks to more than the size limit")
	}
	return n, nil
}

func (u *unpacker) warn(text string) { u.warnings = append(u.warnings, text) }

// entryName is a zip entry's path as the export's own, or false for one that
// would leave the folder it is unpacked into.
func entryName(name string) (string, bool) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", false
	}
	slashed := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(slashed, "/") || regexp.MustCompile(`^[A-Za-z]:/`).MatchString(slashed) {
		return "", false
	}
	var parts []string
	for _, part := range strings.Split(slashed, "/") {
		switch part {
		case "", ".":
		case "..":
			return "", false
		default:
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return clean(path.Join(parts...)), true
}

func isSystemFile(name string) bool {
	return strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store" || path.Base(name) == "Thumbs.db" ||
		strings.HasPrefix(path.Base(name), "._")
}

// stripWrapper leaves out the folder Notion puts the whole export in
// ("Export-<id>", "Private & Shared"), up to two of them.
func (e *Export) stripWrapper() {
	for i := 0; i < 2; i++ {
		root := ""
		for _, p := range e.Files {
			first, _, found := strings.Cut(p, "/")
			if !found || (root != "" && first != root) {
				return
			}
			root = first
		}
		if root == "" || !wrapper.MatchString(root) {
			return
		}
		disk := map[string]string{}
		for j, p := range e.Files {
			short := strings.TrimPrefix(p, root+"/")
			disk[short] = e.disk[p]
			e.Files[j] = short
		}
		e.disk = disk
		e.Warnings = append(e.Warnings, "the wrapping folder was left out: "+root)
	}
}
