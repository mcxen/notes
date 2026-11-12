package frontend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"notes/internal/settings"
)

func TestPromptHoldsTheAddressKeyAndTools(t *testing.T) {
	work := filepath.Join(t.TempDir(), "Work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	st := settings.Settings{MCP: settings.MCP{Enabled: true, Key: "sk-abc", Vaults: []string{work}, PublicURL: "https://notes.example.com"}}
	p := mcpPrompt(st)
	for _, want := range []string{"https://notes.example.com/mcp", "Authorization: Bearer sk-abc", "notes_search", "notes_update_page", "Work (default: Work)"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt misses %q", want)
		}
	}
	if !strings.Contains(p, "Transport: Streamable HTTP") {
		t.Error("prompt does not name the transport")
	}
}

func TestPromptSaysWhenThereIsNoVault(t *testing.T) {
	st := settings.Settings{MCP: settings.MCP{Key: "sk-abc"}}
	if p := mcpPrompt(st); !strings.Contains(p, "none yet") {
		t.Errorf("no vault: %q", p)
	}
}

func TestReadOnlyIsSaidInThePrompt(t *testing.T) {
	st := settings.Settings{MCP: settings.MCP{Key: "sk-abc", ReadOnly: true}}
	if p := mcpPrompt(st); !strings.Contains(p, "read-only") {
		t.Error("read-only server does not say so")
	}
}

func TestConnectURLFollowsTheSettings(t *testing.T) {
	cases := []struct {
		name string
		mcp  settings.MCP
		want string
	}{
		{"local only", settings.MCP{LocalOnly: true}, "http://127.0.0.1:8999/mcp"},
		{"public address gets the path", settings.MCP{PublicURL: "https://x.example.com/"}, "https://x.example.com/mcp"},
		{"public address already has it", settings.MCP{PublicURL: "https://x.example.com/mcp"}, "https://x.example.com/mcp"},
		{"custom port", settings.MCP{LocalOnly: true, Port: 9100}, "http://127.0.0.1:9100/mcp"},
	}
	for _, c := range cases {
		if got := connectURL(settings.Settings{MCP: c.mcp}); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClaudeCommandCarriesTheKey(t *testing.T) {
	cmd := claudeCodeCommand(settings.Settings{MCP: settings.MCP{Key: "sk-abc", LocalOnly: true}})
	if !strings.Contains(cmd, "claude mcp add --transport http notes http://127.0.0.1:8999/mcp") || !strings.Contains(cmd, "Bearer sk-abc") {
		t.Errorf("command: %s", cmd)
	}
}

func TestMaskedKeyNeverShowsTheWhole(t *testing.T) {
	key := "sk-0123456789abcdef0123456789abcdef01234567"
	m := maskedKey(key)
	if strings.Contains(m, "0123456789abcdef0123456789abcdef") || !strings.HasPrefix(m, "sk-0") {
		t.Errorf("masked %q", m)
	}
}

func TestPortDefaultsTo8999(t *testing.T) {
	if (settings.MCP{}).PortOrDefault() != 8999 || (settings.MCP{Port: 9100}).PortOrDefault() != 9100 {
		t.Error("the default port is not 8999")
	}
}
