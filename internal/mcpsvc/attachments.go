package mcpsvc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"notes/internal/markdown"
	"notes/internal/vault"
)

// Pictures and files on a page. notes_fetch lists them; notes_read_attachment
// gives a picture to the assistant as an image, and a file as text or bytes;
// notes_add_attachment puts one on a page.

const (
	// maxRead is the largest file handed over as it is. A larger picture is
	// read as its preview, when it has one.
	maxRead = 5 << 20
	// maxText is how much of a text file is handed over.
	maxText = 1 << 20
	// maxAdd is the largest file that may be added in one call: sent as
	// base64, it must stay under the 4 MB the server takes in a request.
	maxAdd = 5 << 19
	// localOwner is the owner the app gives files written on this computer.
	localOwner = "local-user"
)

type attachmentOut struct {
	BlockID string `json:"block_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	// Picture says that notes_read_attachment shows it as an image.
	Picture bool   `json:"picture"`
	Path    string `json:"path" jsonschema:"pass this to notes_read_attachment"`
}

// attachmentsOf lists what a page's paragraphs hold, in reading order.
func attachmentsOf(blocks []markdown.Block) []attachmentOut {
	list := append([]markdown.Block(nil), blocks...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Order < list[j].Order })
	out := []attachmentOut{}
	seen := map[string]bool{}
	for _, b := range list {
		for _, a := range b.Attachments() {
			if seen[a.Path] {
				continue
			}
			seen[a.Path] = true
			out = append(out, attachmentOut{BlockID: a.BlockID, Name: a.Name, Type: a.Type, Size: a.Size,
				Picture: rasterImage(a.Type), Path: a.Path})
		}
	}
	return out
}

// rasterImage is a picture the assistants are shown as an image.
func rasterImage(mediaType string) bool {
	switch strings.ToLower(mediaType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

func textual(mediaType string) bool {
	mediaType = strings.ToLower(mediaType)
	return strings.HasPrefix(mediaType, "text/") || strings.HasSuffix(mediaType, "+xml") || strings.HasSuffix(mediaType, "+json") ||
		mediaType == "application/json" || mediaType == "application/xml" || mediaType == "application/javascript" ||
		mediaType == "image/svg+xml" || mediaType == "application/x-yaml"
}

func sizeWords(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

type readAttachmentIn struct {
	Vault  string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID string `json:"page_id" jsonschema:"the page the picture or file is on"`
	Path   string `json:"path" jsonschema:"the attachment's path, from the attachments of notes_fetch"`
}

type readAttachmentOut struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Note string `json:"note,omitempty"`
}

func (s *server) readAttachment(_ context.Context, _ *mcp.CallToolRequest, in readAttachmentIn) (*mcp.CallToolResult, readAttachmentOut, error) {
	var out readAttachmentOut
	var result *mcp.CallToolResult
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		if _, ok := lib.pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		// Only what the page holds is given out, never a path of the caller's own.
		var found *markdown.Attachment
		for _, b := range lib.blocks[in.PageID] {
			for _, a := range b.Attachments() {
				if a.Path == in.Path {
					a := a
					found = &a
				}
			}
		}
		if found == nil {
			return errors.New("this page holds no such file: use a path from the attachments of notes_fetch")
		}
		serve, note := found.Path, ""
		full, err := v.AttachmentPath(serve)
		if err != nil {
			return err
		}
		info, err := os.Stat(full)
		if err != nil {
			return fmt.Errorf("the file is not in this vault's attachments (it may not have been synced here yet): %w", err)
		}
		if info.Size() > maxRead && rasterImage(found.Type) && found.Preview != "" {
			serve = found.Preview
			note = fmt.Sprintf("the picture is %s, so this is its smaller preview", sizeWords(info.Size()))
		}
		data, err := v.ReadFile(serve)
		if err != nil {
			return err
		}
		if len(data) > maxRead && !textual(found.Type) {
			return fmt.Errorf("the file is %s, over the %s limit, and has no smaller preview", sizeWords(int64(len(data))), sizeWords(maxRead))
		}
		kind := found.Type
		if sniffed := http.DetectContentType(data); rasterImage(sniffed) || kind == "" || kind == "application/octet-stream" {
			kind = sniffed
		}
		out = readAttachmentOut{Name: found.Name, Type: kind, Size: int64(len(data)), Note: note}
		head := &mcp.TextContent{Text: fmt.Sprintf("%s (%s, %s)", found.Name, kind, sizeWords(int64(len(data))))}
		switch {
		case rasterImage(kind):
			result = &mcp.CallToolResult{Content: []mcp.Content{head, &mcp.ImageContent{Data: data, MIMEType: kind}}}
		case textual(kind) || strings.HasPrefix(kind, "text/"):
			text := data
			if len(text) > maxText {
				text = text[:maxText]
				out.Note = strings.TrimSpace(out.Note + " the text is cut at " + sizeWords(maxText))
			}
			result = &mcp.CallToolResult{Content: []mcp.Content{head, &mcp.TextContent{Text: string(text)}}}
		default:
			result = &mcp.CallToolResult{Content: []mcp.Content{head, &mcp.EmbeddedResource{
				Resource: &mcp.ResourceContents{URI: "notes://" + found.Path, MIMEType: kind, Blob: data}}}}
		}
		return nil
	})
	return result, out, err
}

type addAttachmentIn struct {
	Vault  string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID string `json:"page_id" jsonschema:"the page to put it on"`
	Name   string `json:"name" jsonschema:"the file's name, with its extension, such as diagram.png"`
	Data   string `json:"data" jsonschema:"the file's bytes, base64-encoded (up to 2.5 MB)"`
	Type   string `json:"type,omitempty" jsonschema:"the media type, such as image/png; found from the bytes or the name when omitted"`
}

type addAttachmentOut struct {
	BlockID string `json:"block_id"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
}

var (
	mediaType = regexp.MustCompile(`^[\w.+-]{1,60}/[\w.+-]{1,60}$`)
	unsafe    = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	pageDir   = regexp.MustCompile(`[^\w-]`)
)

// shownName is a file's own name, as the page shows it: the last part of a
// path, in whatever script it is written, without control characters.
func shownName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if chars := []rune(name); len(chars) > 255 {
		name = string(chars[:255])
	}
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

// safeName is a file's name as the attachments folder takes it: letters,
// digits, dots, dashes and underscores. A name in another script keeps its
// extension ("会议纪要.pdf" is stored as file.pdf, and shown by its own name).
func safeName(name string) string {
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, `\`, "/")))
	ext := strings.Trim(unsafe.ReplaceAllString(path.Ext(name), "_"), "._")
	if len(ext) > 12 {
		ext = ""
	}
	stem := strings.Trim(unsafe.ReplaceAllString(strings.TrimSuffix(name, path.Ext(name)), "_"), "._")
	if stem == "" {
		stem = "file"
	}
	if len(stem) > 100 {
		stem = stem[:100]
	}
	if ext == "" {
		return stem
	}
	return stem + "." + ext
}

func (s *server) addAttachment(_ context.Context, _ *mcp.CallToolRequest, in addAttachmentIn) (*mcp.CallToolResult, addAttachmentOut, error) {
	var out addAttachmentOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	encoded := in.Data
	if i := strings.Index(encoded, ";base64,"); strings.HasPrefix(encoded, "data:") && i > 0 {
		encoded = encoded[i+len(";base64,"):]
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(encoded))
	}
	if err != nil {
		return nil, out, errors.New("data is not valid base64")
	}
	if len(data) == 0 {
		return nil, out, errors.New("the file is empty")
	}
	if len(data) > maxAdd {
		return nil, out, fmt.Errorf("the file is %s, over the %s limit", sizeWords(int64(len(data))), sizeWords(maxAdd))
	}
	name, stored := shownName(in.Name), safeName(in.Name)
	kind := strings.ToLower(in.Type)
	if !mediaType.MatchString(kind) {
		kind = http.DetectContentType(data)
		if kind == "application/octet-stream" || strings.HasPrefix(kind, "text/plain") {
			if byName := mime.TypeByExtension(path.Ext(stored)); byName != "" {
				kind = strings.ToLower(strings.SplitN(byName, ";", 2)[0])
			}
		}
		kind = strings.ToLower(strings.SplitN(kind, ";", 2)[0])
	}
	err = s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		page, ok := lib.pages[in.PageID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		if lib.trashed(in.PageID) {
			return errors.New("the page is in the Trash: restore it first (notes_restore_page)")
		}
		now := stamp()
		filePath := "files/" + pageDir.ReplaceAllString(in.PageID, "") + "/" + newID("file") + "/" + stored
		file := map[string]any{"path": filePath, "owner": localOwner, "name": name, "size": len(data), "type": kind}
		if cfg, _, err := image.DecodeConfig(strings.NewReader(string(data))); err == nil && cfg.Width > 0 && cfg.Height > 0 {
			file["width"], file["height"] = cfg.Width, cfg.Height
		}
		blockID := newID("block")
		block := markdown.Block{ID: blockID, Order: 1}
		if rasterImage(kind) || kind == "image/svg+xml" || kind == "image/avif" || kind == "image/bmp" {
			// A picture stands on a line of its own, as one dropped on a page does.
			block.Kind = "text"
			block.Runs = []markdown.Run{{Text: "⁣", Mention: map[string]any{"kind": "image", "file": file}}}
		} else {
			block.Kind = "file"
			block.FileRef = file
		}
		var writes []vault.Write
		cp, err := checkpoint(v, in.PageID, page, lib.stored[in.PageID], now)
		if err != nil {
			return err
		}
		writes = append(writes, cp...)
		writes = append(writes, bodyWrites(in.PageID, []markdown.Block{block}, lastOrder(lib.blocks[in.PageID], ""))...)
		writes = append(writes, vault.Write{Action: "patch", Collection: vault.Pages, ID: in.PageID,
			Patch: map[string]any{"updated_at": now, "updated_by": authorName}})
		// The bytes first, so no paragraph ever names a file that is not there.
		if err := v.WriteFile(filePath, data); err != nil {
			return err
		}
		changes, err := apply(v, writes)
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out = addAttachmentOut{BlockID: blockID, Path: filePath, Name: name, Type: kind, Size: int64(len(data))}
		return nil
	})
	return nil, out, err
}
