// Package mcp serves golens over the Model Context Protocol on stdin/stdout.
//
// It is a thin JSON-RPC loop over the same usecase functions as the CLI. The transport is
// newline-delimited JSON-RPC 2.0, as MCP uses for stdio servers; stdout carries protocol
// messages only, so every tool call renders into a buffer first.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

const protocolVersion = "2024-11-05"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsResult struct {
	Tools []tool `json:"tools"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Serve reads newline-delimited JSON-RPC requests from in and writes responses to out.
func Serve(in io.Reader, out io.Writer, engine domain.Engine, version string) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = encoder.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}

		result, rpcErr := handle(req, engine, version)
		if len(req.ID) == 0 {
			// A notification has no response, even when its method is unknown.
			continue
		}
		resp := response{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handle(req request, engine domain.Engine, version string) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return initializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      serverInfo{Name: "golens", Version: version},
		}, nil
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return toolsResult{Tools: toolDefinitions()}, nil
	case "tools/call":
		return callTool(req.Params, engine)
	default:
		return nil, &rpcError{Code: -32601, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
}

func callTool(raw json.RawMessage, engine domain.Engine) (any, *rpcError) {
	var params callParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid tool call params"}
	}

	switch params.Name {
	case "outline":
		var args outlineArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil || args.Path == "" {
			return nil, &rpcError{Code: -32602, Message: "outline needs a path"}
		}
		depth := args.Depth
		if depth == 0 {
			depth = usecase.DepthUnlimited
		}
		return run(func(stdout, _ *bytes.Buffer) error {
			return usecase.OutlineWithFileOptions(engine, args.Path, depth, stdout, opts(args.JSON, args.Compact), fileOpts(args.SkipTestdata, args.SkipIgnored))
		}), nil

	case "get":
		var args getArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil || args.Symbol == "" || args.Target == "" {
			return nil, &rpcError{Code: -32602, Message: "get needs symbol and target"}
		}
		target := args.Target
		if args.PackageDir != "" {
			target = args.PackageDir
		}
		return run(func(stdout, _ *bytes.Buffer) error {
			return usecase.GetWithOptions(engine, args.Symbol, target, !args.BoundsOnly, stdout, opts(args.JSON, args.Compact), resolveOpts(args.SkipTestdata, args.SkipIgnored))
		}), nil

	case "bounds":
		var args boundsArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil || args.Symbol == "" || args.Target == "" {
			return nil, &rpcError{Code: -32602, Message: "bounds needs symbol and target"}
		}
		target := args.Target
		if args.PackageDir != "" {
			target = args.PackageDir
		}
		return run(func(stdout, _ *bytes.Buffer) error {
			return usecase.BoundsWithOptions(engine, args.Symbol, target, stdout, opts(args.JSON, args.Compact), resolveOpts(args.SkipTestdata, args.SkipIgnored))
		}), nil

	case "check":
		var args checkArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil || len(args.Targets) == 0 {
			return nil, &rpcError{Code: -32602, Message: "check needs at least one target"}
		}
		return run(func(stdout, stderr *bytes.Buffer) error {
			return usecase.CheckTargetsWithOptions(engine, args.Targets, usecase.CheckOptions{Tags: args.Tags}, stdout, stderr, opts(args.JSON, args.Compact))
		}), nil

	default:
		return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", params.Name)}
	}
}

func run(call func(stdout, stderr *bytes.Buffer) error) callResult {
	var stdout, stderr bytes.Buffer
	err := call(&stdout, &stderr)

	text := stdout.String()
	if stderr.Len() > 0 {
		if text != "" && text[len(text)-1] != '\n' {
			text += "\n"
		}
		text += stderr.String()
	}
	if err != nil && text == "" {
		text = err.Error()
	}
	return callResult{
		Content: []content{{Type: "text", Text: text}},
		IsError: err != nil,
	}
}

func opts(jsonOut, compact bool) format.Options {
	return format.Options{JSON: jsonOut, Compact: compact}
}

func fileOpts(skipTestdata, skipIgnored bool) usecase.FileOptions {
	return usecase.FileOptions{SkipTestdata: skipTestdata, SkipIgnored: skipIgnored}
}

func resolveOpts(skipTestdata, skipIgnored bool) usecase.ResolveOptions {
	return usecase.ResolveOptions{SkipTestdata: skipTestdata, SkipIgnored: skipIgnored}
}

type outlineArgs struct {
	Path         string `json:"path"`
	Depth        int    `json:"depth"`
	JSON         bool   `json:"json"`
	Compact      bool   `json:"compact"`
	SkipTestdata bool   `json:"skip_testdata"`
	SkipIgnored  bool   `json:"skip_ignored"`
}

type getArgs struct {
	Symbol       string `json:"symbol"`
	Target       string `json:"target"`
	BoundsOnly   bool   `json:"bounds_only"`
	PackageDir   string `json:"package_dir"`
	JSON         bool   `json:"json"`
	Compact      bool   `json:"compact"`
	SkipTestdata bool   `json:"skip_testdata"`
	SkipIgnored  bool   `json:"skip_ignored"`
}

type boundsArgs struct {
	Symbol       string `json:"symbol"`
	Target       string `json:"target"`
	PackageDir   string `json:"package_dir"`
	JSON         bool   `json:"json"`
	Compact      bool   `json:"compact"`
	SkipTestdata bool   `json:"skip_testdata"`
	SkipIgnored  bool   `json:"skip_ignored"`
}

type checkArgs struct {
	Targets []string `json:"targets"`
	Tags    []string `json:"tags"`
	JSON    bool     `json:"json"`
	Compact bool     `json:"compact"`
}

func toolDefinitions() []tool {
	return []tool{
		{
			Name:        "outline",
			Description: "Structural hierarchy map of a Go file or directory.",
			InputSchema: schema(
				[]string{"path"},
				map[string]any{
					"path":          map[string]any{"type": "string", "description": "file or directory"},
					"depth":         map[string]any{"type": "integer", "description": "recursion depth; 0 means unlimited"},
					"json":          map[string]any{"type": "boolean"},
					"compact":       map[string]any{"type": "boolean"},
					"skip_testdata": map[string]any{"type": "boolean"},
					"skip_ignored":  map[string]any{"type": "boolean"},
				},
			),
		},
		{
			Name:        "get",
			Description: "Exact source of one declaration, or only its bounds with bounds_only.",
			InputSchema: schema(
				[]string{"symbol", "target"},
				map[string]any{
					"symbol":        map[string]any{"type": "string"},
					"target":        map[string]any{"type": "string"},
					"bounds_only":   map[string]any{"type": "boolean"},
					"package_dir":   map[string]any{"type": "string"},
					"json":          map[string]any{"type": "boolean"},
					"compact":       map[string]any{"type": "boolean"},
					"skip_testdata": map[string]any{"type": "boolean"},
					"skip_ignored":  map[string]any{"type": "boolean"},
				},
			),
		},
		{
			Name:        "bounds",
			Description: "Byte and line offsets for one declaration.",
			InputSchema: schema(
				[]string{"symbol", "target"},
				map[string]any{
					"symbol":        map[string]any{"type": "string"},
					"target":        map[string]any{"type": "string"},
					"package_dir":   map[string]any{"type": "string"},
					"json":          map[string]any{"type": "boolean"},
					"compact":       map[string]any{"type": "boolean"},
					"skip_testdata": map[string]any{"type": "boolean"},
					"skip_ignored":  map[string]any{"type": "boolean"},
				},
			),
		},
		{
			Name:        "check",
			Description: "Structural validation of one or more files or directories.",
			InputSchema: schema(
				[]string{"targets"},
				map[string]any{
					"targets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"json":    map[string]any{"type": "boolean"},
					"compact": map[string]any{"type": "boolean"},
				},
			),
		},
	}
}

func schema(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   required,
		"properties": properties,
	}
}
