// Package settings keeps the app's own settings: which vault folders the user
// has opened, most recent first. It reads and writes one JSON file, and it
// has no dependency on the window layer.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// MaxRecent is how many vault folders are remembered.
const MaxRecent = 12

// Remote is the remote folder a vault's working copy is synced with. The
// password is not here: it is in the system's credential store.
type Remote struct {
	// Kind is "webdav" or "smb".
	Kind string `json:"kind"`
	// Address is the folder's URL for WebDAV, or the host for SMB.
	Address string `json:"address"`
	// Share and Folder locate an SMB folder: the share, then the folder in it.
	Share  string `json:"share,omitempty"`
	Folder string `json:"folder,omitempty"`
	User   string `json:"user,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// Settings is the app's settings file.
type Settings struct {
	// Recent lists vault folders, most recent first.
	Recent []string `json:"recent"`
	// Remotes maps a working copy folder to the remote folder it syncs with.
	Remotes map[string]Remote `json:"remotes,omitempty"`
	// PickerAtLaunch shows the vault picker at launch instead of reopening the
	// last vault.
	PickerAtLaunch bool `json:"pickerAtLaunch,omitempty"`
	// OpaqueSidebar turns off the translucent sidebar. It is translucent unless
	// this is set, as the design asks.
	OpaqueSidebar bool `json:"opaqueSidebar,omitempty"`
	// UpdateSource is where updates are downloaded from: "" picks by language,
	// "github" or "tvv" (the tvv.tw mirror) force one.
	UpdateSource string `json:"updateSource,omitempty"`
	// HideOutline turns off the page outline (the short lines at a page's right
	// edge). It is shown unless this is set.
	HideOutline bool `json:"hideOutline,omitempty"`
	// Language is the language the app shows: "" follows the system, else a
	// tag the app speaks ("en", "zh-Hans").
	Language string `json:"language,omitempty"`
	// MCP is the Model Context Protocol server that lets AI assistants read and
	// edit the vaults it is allowed. Off unless Enabled.
	MCP MCP `json:"mcp,omitempty"`
}

// DefaultMCPPort is the port the MCP server listens on unless another is set.
const DefaultMCPPort = 8999

// MCP is the settings of the Model Context Protocol server.
type MCP struct {
	// Enabled turns the server on. It is off by default.
	Enabled bool `json:"enabled,omitempty"`
	// Key is the secret an assistant sends as "Authorization: Bearer <key>".
	// The settings file is kept readable by its owner only.
	Key string `json:"key,omitempty"`
	// Port is the TCP port; 0 means DefaultMCPPort.
	Port int `json:"port,omitempty"`
	// LocalOnly listens on 127.0.0.1 only. Off, the server listens on every
	// network interface, so other computers and a proxy (frp, a tunnel) can reach
	// it; the key is required either way.
	LocalOnly bool `json:"localOnly,omitempty"`
	// PublicURL is the address the server is reached at from outside, copied
	// into the prompt. Optional.
	PublicURL string `json:"publicURL,omitempty"`
	// ReadOnly lets assistants read but not change anything.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Vaults are the folders the assistants may use. The first is the default.
	Vaults []string `json:"vaults,omitempty"`
}

// PortOrDefault is the port to listen on.
func (m MCP) PortOrDefault() int {
	if m.Port == 0 {
		return DefaultMCPPort
	}
	return m.Port
}

// SetRemote records the remote folder of a working copy.
func (s *Settings) SetRemote(folder string, r Remote) {
	if s.Remotes == nil {
		s.Remotes = map[string]Remote{}
	}
	s.Remotes[folder] = r
}

// RemoveRemote forgets the remote folder of a working copy. The working copy
// stays a local vault.
func (s *Settings) RemoveRemote(folder string) {
	delete(s.Remotes, folder)
}

// RemoteOf returns the remote folder of a working copy, if it has one.
func (s Settings) RemoteOf(folder string) (Remote, bool) {
	r, ok := s.Remotes[folder]
	return r, ok
}

// Load reads the settings file. A missing file is empty settings.
func Load(path string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	return s, nil
}

// Save writes the settings file, replacing the old one in one step.
func (s Settings) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	// Private to the owner: it holds the key of the MCP server.
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Remember moves folder to the front of the recent list.
func (s *Settings) Remember(folder string) {
	s.Recent = slices.DeleteFunc(s.Recent, func(f string) bool { return f == folder })
	s.Recent = append([]string{folder}, s.Recent...)
	if len(s.Recent) > MaxRecent {
		s.Recent = s.Recent[:MaxRecent]
	}
}

// Forget removes folder from the recent list.
func (s *Settings) Forget(folder string) {
	s.Recent = slices.DeleteFunc(s.Recent, func(f string) bool { return f == folder })
}
