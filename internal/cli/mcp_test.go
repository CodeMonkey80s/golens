package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
)

func TestMCPCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	root := New(Env{
		Stdin:  strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"),
		Stdout: &out,
		Stderr: &errOut,
		Engine: goast.New(),
	})
	root.SetArgs([]string{"mcp"})
	if code := Execute(root); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errOut.String())
	}

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out.String())
	}
	if resp.JSONRPC != "2.0" || resp.Result.ProtocolVersion == "" {
		t.Errorf("initialize response = %+v", resp)
	}
}
