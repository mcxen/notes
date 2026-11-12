package mcpsvc

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdioCarriesRequestsAndAnswersOneLineEach(t *testing.T) {
	ts := httptest.NewServer(NewHandler(testHost{folders: []string{t.TempDir()}}, "sk-test", "test"))
	defer ts.Close()
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out, errw bytes.Buffer
	if err := RunStdio(in, &out, &errw, ts.URL+"/mcp", "sk-test"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines out, want 2 (the notification has no answer):\n%s", len(lines), out.String())
	}
	for i, line := range lines {
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("line %d is not one JSON message: %v", i, err)
		}
		if msg["id"] != float64(i+1) {
			t.Errorf("line %d answers id %v", i, msg["id"])
		}
	}
}

func TestStdioAnswersAWrongKeyWithAnErrorForThatRequest(t *testing.T) {
	ts := httptest.NewServer(NewHandler(testHost{folders: []string{t.TempDir()}}, "sk-test", "test"))
	defer ts.Close()
	in := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")
	var out, errw bytes.Buffer
	if err := RunStdio(in, &out, &errw, ts.URL+"/mcp", "sk-wrong"); err != nil {
		t.Fatal(err)
	}
	var msg struct {
		ID    float64 `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &msg); err != nil {
		t.Fatalf("no error answer: %v\n%s", err, out.String())
	}
	if msg.ID != 7 || !strings.Contains(msg.Error.Message, "401") {
		t.Fatalf("error answer: %+v", msg)
	}
	if errw.Len() == 0 {
		t.Error("the reason was not logged to stderr")
	}
}
