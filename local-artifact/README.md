# local-artifact-mcp

A **completely local** MCP server that lets agents **save and retrieve named artifacts** (text, files, images) without running a web service.

## Storage format

By default artifacts are stored under:

- Linux/macOS: `~/.local/share/ccsubagents/artifacts`
- Override with `ARTIFACT_STORE_DIR=/path/to/dir`

Directory layout:

```
$ARTIFACT_STORE_DIR/
  <subspace-hash>/         # roots-derived subspace (64 lowercase hex)
    names.json             # name -> latest ref
    objects/<ref>          # raw bytes
    meta/<ref>.json        # Artifact metadata (JSON)
  names.json               # global fallback session store
  objects/<ref>            # global fallback objects
  meta/<ref>.json          # global fallback metadata
```

Each `save_*` creates a new immutable `ref` and updates the `name` pointer in `names.json`.
Aliases are now unique: saving with an existing `name` returns a conflict error instead of overwriting.

When MCP client roots are available, the server requests `roots/list`, normalizes/sorts root URIs, hashes them with SHA-256, and stores artifacts under `$ARTIFACT_STORE_DIR/<hash>/`. On `roots/list` errors `-32601` or `-32603`, the server falls back to the global store (`$ARTIFACT_STORE_DIR/`) for that process session only.

## Exposed MCP tools

- `save_artifact_text`
- `save_artifact_blob` (binary base64)
- `resolve_artifact`
- `get_artifact`
- `get_artifact_list`
- `delete_artifact`

## Build

```
go build ./cmd/artifact-mcp
go build ./cmd/artifact-web
```

## Web UI (optional)

Run a simple local web UI to inspect and delete current artifacts:

```
ARTIFACT_WEB_ADDR=127.0.0.1:19130 go run ./cmd/artifact-web
```

Then open `http://127.0.0.1:19130`.

The web UI includes a subspace selector (detected from hash directories) and the API supports:

- `GET /api/subspaces`
- `GET /api/artifacts?subspace=<64-hex>[&prefix=...&limit=...]`
- `DELETE /api/artifacts?subspace=<64-hex>&name=...` (or `ref=...`)

## Example usage pattern for CCSubAgents

1. Planner calls `save_artifact_text` with name `plan/task-123`.
2. Orchestrator passes only the returned `ref` or `artifact://name/...` URI to the implementation subagent.
3. Implementation subagent calls `get_artifact` (or `resources/read`) to load the plan when needed.
