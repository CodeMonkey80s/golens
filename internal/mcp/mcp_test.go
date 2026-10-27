package mcp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/mcp"
)

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func serve(t *testing.T, lines ...string) []rpcResponse {
	t.Helper()
	var out bytes.Buffer
	if err := mcp.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, goast.New(), "test"); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var responses []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("response is not JSON: %v\n%s", err, line)
		}
		responses = append(responses, resp)
	}
	return responses
}

func contentText(t *testing.T, resp rpcResponse) string {
	t.Helper()
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("tool result: %v\n%s", err, resp.Result)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("content = %+v, want one text item", result.Content)
	}
	return result.Content[0].Text
}

func TestInitializeAndToolsList(t *testing.T) {
	responses := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want 2", len(responses))
	}

	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(responses[0].Result, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion == "" || init.ServerInfo.Name != "golens" {
		t.Errorf("initialize result = %+v", init)
	}

	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(responses[1].Result, &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"outline", "get", "bounds", "check"} {
		if !names[want] {
			t.Errorf("tools/list is missing %q", want)
		}
	}
}

func TestToolCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte("package p\n\ntype T struct{ A int }\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unused := filepath.Join(dir, "unused.go")
	if err := os.WriteFile(unused, []byte("package p\n\nimport \"fmt\"\n\nfunc G() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"path": path, "depth": 1, "json": true})
	getArgs, _ := json.Marshal(map[string]any{"symbol": "F", "target": path})
	checkArgs, _ := json.Marshal(map[string]any{"targets": []string{unused}})

	responses := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"outline","arguments":`+string(args)+`}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get","arguments":`+string(getArgs)+`}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"check","arguments":`+string(checkArgs)+`}}`,
	)
	if len(responses) != 3 {
		t.Fatalf("responses = %d, want 3", len(responses))
	}

	if text := contentText(t, responses[0]); !strings.Contains(text, `"package": "p"`) {
		t.Errorf("outline text:\n%s", text)
	}
	if text := contentText(t, responses[1]); !strings.Contains(text, "func F()") {
		t.Errorf("get text:\n%s", text)
	}

	var checkResult struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(responses[2].Result, &checkResult); err != nil {
		t.Fatal(err)
	}
	if !checkResult.IsError {
		t.Error("check diagnostics should be isError=true")
	}
	if text := contentText(t, responses[2]); !strings.Contains(text, "imported and not used") {
		t.Errorf("check text:\n%s", text)
	}
}

func TestErrorsAndNotifications(t *testing.T) {
	responses := serve(t,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"unknown/method"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want 2 (the notification must be silent)", len(responses))
	}
	if responses[0].Error == nil || responses[0].Error.Code != -32601 {
		t.Errorf("unknown method error = %+v", responses[0].Error)
	}
	if responses[1].Error == nil || responses[1].Error.Code != -32602 {
		t.Errorf("unknown tool error = %+v", responses[1].Error)
	}
}
