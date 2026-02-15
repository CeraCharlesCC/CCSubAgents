# local-artifact-mcp

A **completely local** MCP server that lets agents **save and retrieve named artifacts** (text, files, images) without running a web service.

## Storage format

By default artifacts are stored under:

- Linux/macOS: `~/.local/share/ccsubagents/artifacts`
- Override with `ARTIFACT_STORE_DIR=/path/to/dir`

Directory layout:

```
$ARTIFACT_STORE_DIR/
  names.json               # name -> latest ref
  objects/<ref>            # raw bytes
  meta/<ref>.json          # Artifact metadata (JSON)
```

Each `save_*` creates a new immutable `ref` and updates the `name` pointer in `names.json`.
Aliases are now unique: saving with an existing `name` returns a conflict error instead of overwriting.

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

## Example usage pattern for CCSubAgents

1. Planner calls `save_artifact_text` with name `plan/task-123`.
2. Orchestrator passes only the returned `ref` or `artifact://name/...` URI to the implementation subagent.
3. Implementation subagent calls `get_artifact` (or `resources/read`) to load the plan when needed.
