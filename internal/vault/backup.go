package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ImportReport says what an import brought into the vault.
type ImportReport struct {
	Pages       int `json:"pages"`
	Blocks      int `json:"blocks"`
	Versions    int `json:"versions"`
	Settings    int `json:"settings"`
	Attachments int `json:"attachments"`
	// Unplaced counts records whose ids name no page, so they belong nowhere.
	// They are left out.
	Unplaced int `json:"unplaced"`
	// Orphans counts paragraphs whose page the backup does not hold. They are
	// kept, under their page id.
	Orphans int `json:"orphans"`
}

// backupJSON is the browser's backup (web/src/platform/backup.ts).
type backupJSON struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Records []struct {
		Scope      string     `json:"scope"`
		Collection Collection `json:"collection"`
		ID         string     `json:"id"`
		Value      Record     `json:"value"`
	} `json:"records"`
	Files []struct {
		Key  string `json:"key"`
		Type string `json:"type"`
		Data string `json:"data"`
	} `json:"files"`
}

const backupPersonalPrefix = "personal\n"

var errNotBackup = errors.New("Choose an Open Notes backup JSON file.")

// ImportBackup merges a browser backup into the vault. Every record and file
// is checked before anything is written, so a bad backup changes nothing.
// Records already in the vault with the same id are replaced.
func (v *Vault) ImportBackup(data []byte) (ImportReport, error) {
	var backup backupJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&backup); err != nil {
		return ImportReport{}, errNotBackup
	}
	if backup.Format != "open-notes" || backup.Version != 1 || backup.Records == nil || backup.Files == nil {
		return ImportReport{}, errNotBackup
	}

	report := ImportReport{}
	pagesInBackup := map[string]bool{}
	seen := map[string]bool{}
	for _, row := range backup.Records {
		if row.Scope != "personal" || row.ID == "" || row.Value == nil {
			return report, errors.New("The backup contains an invalid record.")
		}
		switch row.Collection {
		case Pages, Blocks, Versions, Settings:
		default:
			return report, errors.New("The backup contains an invalid record.")
		}
		key := string(row.Collection) + "\x00" + row.ID
		if seen[key] {
			return report, errors.New("The backup contains duplicate records.")
		}
		seen[key] = true
		if row.Collection == Pages {
			pagesInBackup[row.ID] = true
		}
	}

	type planned struct{ write Write }
	var writes []planned
	for _, row := range backup.Records {
		ref := Ref{Collection: row.Collection, ID: row.ID}
		if validRef(ref) != nil {
			report.Unplaced++
			continue
		}
		if ref.Collection == Blocks {
			pageID, _, _ := ParseBlockRecordID(row.ID)
			if page, ok := row.Value["page"]; ok && page != pageID {
				report.Unplaced++
				continue
			}
			if !pagesInBackup[pageID] {
				report.Orphans++
			}
		}
		writes = append(writes, planned{Write{Action: "put", Collection: row.Collection, ID: row.ID, Value: row.Value}})
	}

	type file struct {
		path string
		data []byte
	}
	var files []file
	for _, item := range backup.Files {
		if !strings.HasPrefix(item.Key, backupPersonalPrefix) {
			return report, errors.New("The backup contains an invalid attachment.")
		}
		path := strings.TrimPrefix(item.Key, backupPersonalPrefix)
		if _, err := attachmentRel(path); err != nil {
			return report, errors.New("The backup contains an invalid attachment.")
		}
		bytesOf, err := base64.StdEncoding.DecodeString(item.Data)
		if err != nil {
			return report, errors.New("The backup contains an invalid attachment.")
		}
		files = append(files, file{path: path, data: bytesOf})
	}

	for _, p := range writes {
		if _, err := v.Write(p.write); err != nil {
			return report, fmt.Errorf("import: %w", err)
		}
		switch p.write.Collection {
		case Pages:
			report.Pages++
		case Blocks:
			report.Blocks++
		case Versions:
			report.Versions++
		case Settings:
			report.Settings++
		}
	}
	for _, f := range files {
		if err := v.WriteFile(f.path, f.data); err != nil {
			return report, fmt.Errorf("import: %w", err)
		}
		report.Attachments++
	}
	if err := v.Flush(); err != nil {
		return report, err
	}
	return report, nil
}
