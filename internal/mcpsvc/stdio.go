package mcpsvc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultProtocol is the protocol version a stdio carrier speaks until the
// client's initialize says another.
const DefaultProtocol = "2025-06-18"

// RunStdio carries a client's stdio to the server at endpoint, with key as its
// bearer key: each line the client writes is a request or a notification, and
// each reply is written back as one line. A client that speaks only stdio uses
// this; the app has to be running with the server on. Logs go to errw.
func RunStdio(in io.Reader, out, errw io.Writer, endpoint, key string) error {
	c := &carrier{endpoint: endpoint, key: key, out: out, errw: errw, protocol: DefaultProtocol, client: &http.Client{}}
	r := bufio.NewReaderSize(in, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if msg := bytes.TrimSpace(line); len(msg) > 0 {
			c.forward(msg)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type carrier struct {
	endpoint string
	key      string
	out      io.Writer
	errw     io.Writer
	protocol string
	client   *http.Client
}

func (c *carrier) forward(msg []byte) {
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(msg, &head); err != nil {
		c.reply(nil, -32700, "parse error: "+err.Error())
		return
	}
	// Only a request expects an answer.
	wantsReply := head.Method != "" && len(head.ID) > 0 && string(head.ID) != "null"

	req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(msg))
	if err != nil {
		c.fail(head.ID, wantsReply, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("MCP-Protocol-Version", c.protocol)
	resp, err := c.client.Do(req)
	if err != nil {
		c.fail(head.ID, wantsReply, fmt.Errorf("Notes is not reachable at %s: %w (is the app running, with MCP turned on in Settings › Integrations?)", c.endpoint, err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		c.fail(head.ID, wantsReply, fmt.Errorf("the server answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		for _, data := range sseData(body) {
			c.write(data)
		}
		return
	}
	if head.Method == "initialize" {
		var answer struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &answer) == nil && answer.Result.ProtocolVersion != "" {
			c.protocol = answer.Result.ProtocolVersion
		}
	}
	if len(bytes.TrimSpace(body)) > 0 {
		c.write(body)
	}
}

// fail answers a request that could not be carried, and logs why.
func (c *carrier) fail(id json.RawMessage, wantsReply bool, err error) {
	fmt.Fprintln(c.errw, "notes mcp:", err)
	if wantsReply {
		c.reply(id, -32000, err.Error())
	}
}

func (c *carrier) reply(id json.RawMessage, code int, message string) {
	if id == nil {
		id = json.RawMessage("null")
	}
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	c.write(data.Bytes())
}

// write sends one message as one line.
func (c *carrier) write(data []byte) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		compact.Reset()
		compact.Write(bytes.ReplaceAll(data, []byte("\n"), []byte(" ")))
	}
	compact.WriteByte('\n')
	_, _ = c.out.Write(compact.Bytes())
}

// sseData is the data of each event in a server-sent stream.
func sseData(body []byte) [][]byte {
	var out [][]byte
	for _, line := range strings.Split(string(body), "\n") {
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			out = append(out, []byte(strings.TrimSpace(data)))
		}
	}
	return out
}
