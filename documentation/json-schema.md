# golens JSON Schema

Formal schema for all JSON payloads produced by golens commands. Every payload
carries `"schema_version": 1` as its first field.

---

## Common Fields

| Field | Type | Description |
|-------|------|-------------|
| `schema_version` | integer | Always `1`. First field in every payload. |

---

## 1. `golens outline --json`

Always an array of per-file objects, even for a single file.

```json
[
  {
    "schema_version": 1,
    "file": "/path/to/file.go",
    "package": "store",
    "build": "windows",
    "nodes": [...]
  }
]
```

### File Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema_version` | integer | yes | Always `1` |
| `file` | string | yes | File path, as named by the caller |
| `package` | string | yes | Package name |
| `build` | string | no | `//go:build` expression, present only when the file has one |
| `nodes` | array[Node] | yes | Declaration nodes (may be empty) |
| `imports` | array[string] | no | Import paths, present only when the file has imports |

### Node Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Declaration name |
| `kind` | string | yes | `type`, `func`, `method`, `field`, `var`, `const`, `import` |
| `detail` | string | no | Type/kind detail (e.g., `struct`, `[]int`, `s *UserStore`) |
| `type_params` | string | no | Generic type parameters verbatim (e.g., `[T any]`) |
| `receiver` | string | no | Receiver as spelled in source (e.g., `s *UserStore`) |
| `is_alias` | boolean | no | `true` for `type A = B`, omitted otherwise |
| `start_line` | integer | yes | Inclusive line number |
| `end_line` | integer | yes | Inclusive line number |
| `children` | array[Node] | no | Child nodes (fields, parameters, interface methods) |

`is_alias` is omitted when `false`. `type_params`, `receiver` are omitted when empty.
`children` is omitted when empty.

### Node Kind Values

| Kind | Description | Can Have Children |
|------|-------------|-------------------|
| `type` | Type declaration (struct, interface, alias, etc.) | Yes (fields, methods) |
| `func` | Function declaration | Yes (parameters) |
| `method` | Method declaration | Yes (parameters) |
| `field` | Struct field or parameter | No |
| `var` | Variable declaration | No |
| `const` | Constant declaration | No |
| `import` | Import declaration | No |

---

## 2. `golens get --json`

A single JSON object:

```json
{
  "schema_version": 1,
  "symbol": "UserStore.GetUser",
  "file": "/path/to/file.go",
  "start_line": 17,
  "end_line": 25,
  "start_byte": 430,
  "end_byte": 612,
  "source": "func (s *UserStore) GetUser(...) {...}"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema_version` | integer | yes | Always `1` |
| `symbol` | string | yes | Symbol name (e.g., `UserStore.GetUser`, `MyFunc`) |
| `file` | string | yes | File path |
| `start_line` | integer | yes | Inclusive line number |
| `end_line` | integer | yes | Inclusive line number |
| `start_byte` | integer | yes | Half-open start byte offset |
| `end_byte` | integer | yes | Half-open end byte offset |
| `source` | string | yes | Exact source bytes of the symbol |

Omit `source` when `--bounds-only` is used (then identical to `bounds`).

---

## 3. `golens bounds --json`

A single JSON object:

```json
{
  "schema_version": 1,
  "symbol": "UserStore.GetUser",
  "file": "/path/to/file.go",
  "start_line": 17,
  "end_line": 25,
  "start_byte": 430,
  "end_byte": 612
}
```

Identical to `get --bounds-only --json` without the `source` field.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema_version` | integer | yes | Always `1` |
| `symbol` | string | yes | Symbol name |
| `file` | string | yes | File path |
| `start_line` | integer | yes | Inclusive line number |
| `end_line` | integer | yes | Inclusive line number |
| `start_byte` | integer | yes | Half-open start byte offset |
| `end_byte` | integer | yes | Half-open end byte offset |

---

## 4. `golens check --json`

A single JSON object:

```json
{
  "schema_version": 1,
  "diagnostics": [...]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema_version` | integer | yes | Always `1` |
| `diagnostics` | array[Diagnostic] | yes | Empty array `[]` when clean |

### Diagnostic Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `severity` | string | yes | `"error"` or `"warning"` |
| `file` | string | yes | File path |
| `line` | integer | yes | Line number (1-based) |
| `column` | integer | yes | Column number (1-based) |
| `message` | string | yes | Diagnostic message |

---

## Exit Codes

| Code | Meaning | stdout | stderr |
|------|---------|--------|--------|
| `0` | Success | payload | empty |
| `1` | Usage error | empty | error message |
| `2` | Not found or ambiguous | empty | error message + candidates |
| `3` | `check` found diagnostics | payload (JSON only) | diagnostics |
| `4` | Parse or I/O failure | payload (JSON, partial) | error message |

---

## Stream Contract

- **`outline`, `get`, `bounds`**: payload → stdout, diagnostics → stderr
- **`check` (human)**: diagnostics → stderr, stdout empty
- **`check --json`**: payload → stdout, stderr empty (do not add `2>&1`)