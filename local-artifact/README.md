# local-artifact-mcp

A **completely local** MCP server that lets agents **save and retrieve named artifacts** (text, files, images) without running a web service.

## Storage format

By default artifacts are stored under:

- Linux/macOS: `~/.local/share/ccsubagents/artifacts`
- Override with `LOCAL_ARTIFACT_STORE_DIR=/path/to/dir`

Directory layout (managed by the `ccsubagentsd` daemon):

```
$LOCAL_ARTIFACT_STORE_DIR/
  registry.sqlite          # maps workspace roots to hex IDs
  blobs/                   # content-addressed raw byte storage
    <hex[:2]>/
      <hex>
  <subspace-hash>/         # roots-derived subspace (64 lowercase hex)
    meta.sqlite            # Artifact metadata (names, refs, relations)
  global/                  # fallback global session subspace
    meta.sqlite
```

Each `save_*` creates a new immutable `ref` and updates the `name` pointer in the corresponding `meta.sqlite`.
Re-saving an existing `name` creates a new latest `ref` and sets `prevRef` to the previous latest `ref`.
Older refs remain retrievable by `ref`.

All MCP/Web requests are routed through a background daemon (`ccsubagentsd`), which ensures safe concurrent access using transactions and handles workspace registry mapping. The MCP server (`local-artifact-mcp`) will automatically spawn the daemon if it's not running.

When MCP client roots are available, the daemon normalizes the root URIs, hashes them with SHA-256, and maintains `meta.sqlite` under `$LOCAL_ARTIFACT_STORE_DIR/<hash>/`. If `roots/list` is unavailable or errors out, the server falls back to the `global` subspace (`$LOCAL_ARTIFACT_STORE_DIR/global/`).

To force workspace separation for MCP clients that do not provide roots/working-directory context (for example Codex CLI), set `LOCAL_ARTIFACT_MCP_OVERRIDE_WORKSPACE_HASH=<64-hex>`. When set, this override is used with highest priority and `roots/list` is not used for workspace selection.

## Exposed MCP tools

- `save_artifact_text`
- `save_artifact_blob` (binary base64)
- `edit_artifact_text`
- `resolve_artifact`
- `get_artifact`
- `get_artifact_list`
- `delete_artifact`
- `todo`

### `edit_artifact_text` tool usage

`edit_artifact_text` edits an **existing** text artifact selected by **exactly one of** `name` or `ref`.
It supports two operations:

- `append` — append `text` to the end of the current artifact body
- `patch` — apply a single-file unified diff `patch`

The target artifact must already exist.
Edits are implemented as read-modify-write operations against the selected version, so they create a new latest `ref`, set `prevRef` to the version they were based on, and fail with a conflict if the edit was based on a stale historical ref rather than the current latest name target.

Append text to an existing artifact:

```json
{
  "name": "edit_artifact_text",
  "arguments": {
    "operation": "append",
    "artifact": {"name": "plan/task-123"},
    "text": "\nImplementation notes..."
  }
}
```

Patch an existing artifact with a unified diff:

```json
{
  "name": "edit_artifact_text",
  "arguments": {
    "operation": "patch",
    "artifact": {"ref": "20260217T010000Z-aaaaaaaaaaaaaaaa"},
    "patch": "--- a/plan.md\n+++ b/plan.md\n@@ -1,2 +1,2 @@\n Step 1\n-Step 2\n+Step 2 (done)\n"
  }
}
```

Patch support is intentionally strict: malformed patches, non-applicable hunks, multi-file diffs, binary patches, and create/delete/rename patch shapes are rejected rather than guessed.

### `todo` tool usage

`todo` stores task state as JSON text under deterministic `<artifact>/todo` names.
The `artifact` selector should reference the base artifact using **exactly one of** `name` or `ref` (for example `plan/task-123`), and the tool derives storage as `<base>/todo`.
It supports three operations:

- `read` — load the current TODO list
- `write` — replace the full TODO list
- `update` — update only one item's `status`

Read TODOs:

```json
{
  "name": "todo",
  "arguments": {
    "operation": "read",
    "artifact": {"name": "plan/task-123"}
  }
}
```

Write TODOs with stale-write protection:

```json
{
  "name": "todo",
  "arguments": {
    "operation": "write",
    "artifact": {"name": "plan/task-123"},
    "todoList": [
      {"id": 1, "title": "Draft", "status": "in-progress"},
      {"id": 2, "title": "Review", "status": "not-started"}
    ],
    "expectedPrevRef": "20260217T010000Z-aaaaaaaaaaaaaaaa"
  }
}
```

Update one TODO item's status by `id`:

```json
{
  "name": "todo",
  "arguments": {
    "operation": "update",
    "artifact": {"name": "plan/task-123"},
    "target": {"id": 2},
    "status": "completed",
    "expectedPrevRef": "20260217T010000Z-aaaaaaaaaaaaaaaa"
  }
}
```

Update one TODO item's status by zero-based array `index`:

```json
{
  "name": "todo",
  "arguments": {
    "operation": "update",
    "artifact": {"name": "plan/task-123"},
    "target": {"index": 0},
    "status": "in-progress"
  }
}
```

`expectedPrevRef` applies to `update` the same way it does to `write`: when provided, it must match the current latest TODO ref or the mutation fails with a conflict and remains non-mutating.

### `get_artifact` and `delete_artifact` selector semantics

- `get_artifact` requires **exactly one of** `name` or `ref`.
- `delete_artifact` requires **exactly one of** `name` or `ref`.

Both tools keep their stable machine-readable metadata in `structuredContent`. `get_artifact` also returns a `resource_link` in success content for client compatibility, but callers should still treat `structuredContent` as the stable machine-readable source for selectors and URIs.

## Build (in /local-artifact/)

```
go build ./cmd/ccsubagentsd
go build ./cmd/local-artifact-mcp
go build ./cmd/local-artifact-web
```

Bootstrap CLI (from repo root):

```
(cd ccsubagents && go build ./cmd/ccsubagents)
```

## Bootstrap installer CLI

The `ccsubagents` binary manages install lifecycle for local CCSubAgents assets.

Commands:

```
ccsubagents install
ccsubagents update
ccsubagents uninstall
ccsubagents doctor
ccsubagents daemon [status|start|stop]
ccsubagents artifacts [ls|get|put]
```

Behavior summary:

- Installs from the latest release in `https://github.com/CeraCharlesCC/CCSubAgents`.
- Verifies downloaded release assets with GitHub attestations before making install/update changes.
- Installs from a runtime-specific bundle asset (`local-artifact_<goos>_<goarch>.zip`) and places `ccsubagentsd`, `local-artifact-mcp`, and `local-artifact-web` (or `.exe` variants on Windows) into `~/.local/bin` by default.
- Extracts `agents.zip` into `~/.local/share/ccsubagents/agents`.
- For `install --scope=global`, prompts for VS Code Desktop (Stable/Insiders), VS Code Server (Stable/Insiders), or custom target path(s).
- Adds `~/.local/share/ccsubagents/agents` to `chat.agentFilesLocations` in the selected `settings.json` target(s) using the object-map format (`"path": true`) without overwriting existing entries.
- Adds/updates only `servers.artifact-mcp` in the selected `mcp.json` target(s), and preserves other keys (including `inputs`).
- Tracks managed files and config insertions in `~/.local/share/ccsubagents/tracked.json` for safe uninstall.

Operational notes:

- `install` and `update` require `gh` CLI in `PATH` for attestation verification (`gh attestation verify`).
- Override install/config paths if needed:
  - `LOCAL_ARTIFACT_BIN_DIR` (default `~/.local/bin`)
  - `LOCAL_ARTIFACT_SETTINGS_PATH` (overrides resolved global `settings.json` target path)
  - `LOCAL_ARTIFACT_MCP_PATH` (overrides resolved global `mcp.json` target path)
- If you point `LOCAL_ARTIFACT_BIN_DIR` to a system path (for example `/usr/local/bin`), elevated privileges may be required.
- `update` forcibly overwrites managed install artifacts to the latest release.
- `uninstall` removes tracked artifacts and reverts only tracked JSON insertions.

## Web UI (optional)

Run a simple local web UI to inspect, insert, and delete current artifacts:

```
LOCAL_ARTIFACT_WEB_UI_ADDR=127.0.0.1:19130 go run ./cmd/local-artifact-web
```

Then open `http://127.0.0.1:19130`.

The web UI includes:

- subspace selection (detected from hash directories)
- manual insertion via text or file upload
- row multi-selection with click/Ctrl(⌘)-click/Shift-click semantics
- bulk delete for selected rows
- a persisted light/dark theme toggle (`localStorage` key: `local-artifact-theme`, defaulting to system preference)

The API supports:

- `GET /api/subspaces`
- `GET /api/artifacts?subspace=<64-hex|global>[&prefix=...&limit=...]`
- `POST /api/artifacts?subspace=<64-hex|global>` with either:
  - text payload: `{ "name": "...", "text": "...", "mimeType": "..." }`
  - blob payload: `{ "name": "...", "dataBase64": "...", "mimeType": "...", "filename": "..." }`
- `DELETE /api/artifacts?subspace=<64-hex|global>&name=...` (or `ref=...`)
  - supports repeated selectors for batch delete, e.g. `&name=a&name=b` or `&ref=...&ref=...`

## Example usage pattern for CCSubAgents

1. Planner calls `save_artifact_text` with name `plan/task-123`.
2. Orchestrator passes only the returned `ref` or `artifact://name/...` URI to the implementation subagent.
3. Implementation subagent calls `get_artifact` (or `resources/read`) to load the plan when needed.
