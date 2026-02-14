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

## Exposed MCP tools

- `artifact.save_text`
- `artifact.save_blob` (binary base64)
- `artifact.resolve`
- `artifact.get`
- `artifact.list`

## Build

```
go build ./cmd/artifact-mcp
```

## Example usage pattern for CCSubAgents

1. Planner calls `artifact.save_text` with name `plan/task-123`.
2. Orchestrator passes only the returned `ref` or `artifact://name/...` URI to the implementation subagent.
3. Implementation subagent calls `artifact.get` (or `resources/read`) to load the plan when needed.

