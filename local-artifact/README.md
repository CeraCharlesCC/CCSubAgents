# local-artifact-mcp

A **completely local** MCP server that lets agents **save and retrieve named artifacts** (text, files, images) without running a web service.

## Storage format

By default artifacts are stored under:

- Linux/macOS: `~/.local/share/ccsubagents/artifacts`
- Override with `LOCAL_ARTIFACT_STORE_DIR=/path/to/dir`

Directory layout:

```
$LOCAL_ARTIFACT_STORE_DIR/
  <subspace-hash>/         # roots-derived subspace (64 lowercase hex)
    names.json             # name -> latest ref
    objects/<ref>          # raw bytes
    meta/<ref>.json        # Artifact metadata (JSON)
  names.json               # global fallback session store
  objects/<ref>            # global fallback objects
  meta/<ref>.json          # global fallback metadata
```

Each `save_*` creates a new immutable `ref` and updates the `name` pointer in `names.json`.
Re-saving an existing `name` creates a new latest `ref` and sets `prevRef` to the previous latest `ref`.
Older refs remain retrievable by `ref`.

When MCP client roots are available, the server requests `roots/list`, normalizes/sorts root URIs, hashes them with SHA-256, and stores artifacts under `$LOCAL_ARTIFACT_STORE_DIR/<hash>/`. If `roots/list` is unavailable, returns any RPC error (including JSON-RPC errors such as `-32601` or `-32603`), cannot be parsed successfully, or if the client does not advertise the roots capability, the server falls back to the global store (`$LOCAL_ARTIFACT_STORE_DIR/`) for that process session only.

## Exposed MCP tools

- `save_artifact_text`
- `save_artifact_blob` (binary base64)
- `resolve_artifact`
- `get_artifact`
- `get_artifact_list`
- `delete_artifact`

## Build (in /local-artifact/)

```
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
```

Behavior summary:

- Installs from the latest release in `https://github.com/CeraCharlesCC/CCSubAgents`.
- Verifies downloaded release assets with GitHub attestations before making install/update changes.
- Installs from `local-artifact.zip` (which contains `local-artifact-mcp` and `local-artifact-web`) and places both binaries into `~/.local/bin` by default.
- Extracts `agents.zip` into `~/.local/share/ccsubagents/agents`.
- Adds `~/.local/share/ccsubagents/agents` to `chat.agentFilesLocations` in `~/.vscode-server-insiders/data/Machine/settings.json` using the object-map format (`"path": true`) without overwriting existing entries.
- Adds/updates only `servers.artifact-mcp` in `~/.vscode-server-insiders/data/User/mcp.json` by default, and preserves other keys (including `inputs`).
- Tracks managed files and config insertions in `~/.local/share/ccsubagents/tracked.json` for safe uninstall.

Operational notes:

- `install` and `update` require `gh` CLI in `PATH` for attestation verification (`gh attestation verify`).
- Override install/config paths if needed:
  - `LOCAL_ARTIFACT_BIN_DIR` (default `~/.local/bin`)
  - `LOCAL_ARTIFACT_SETTINGS_PATH` (default `~/.vscode-server-insiders/data/Machine/settings.json`)
  - `LOCAL_ARTIFACT_MCP_PATH` (default `~/.vscode-server-insiders/data/User/mcp.json`)
- If you point `LOCAL_ARTIFACT_BIN_DIR` to a system path (for example `/usr/local/bin`), elevated privileges may be required.
- `update` forcibly overwrites managed install artifacts to the latest release.
- `uninstall` removes tracked artifacts and reverts only tracked JSON insertions.

## Web UI (optional)

Run a simple local web UI to inspect and delete current artifacts:

```
LOCAL_ARTIFACT_WEB_UI_ADDR=127.0.0.1:19130 go run ./cmd/local-artifact-web
```

Then open `http://127.0.0.1:19130`.

The web UI includes a subspace selector (detected from hash directories) and the API supports:

- `GET /api/subspaces`
- `GET /api/artifacts?subspace=<64-hex|global>[&prefix=...&limit=...]`
- `DELETE /api/artifacts?subspace=<64-hex|global>&name=...` (or `ref=...`)

## Example usage pattern for CCSubAgents

1. Planner calls `save_artifact_text` with name `plan/task-123`.
2. Orchestrator passes only the returned `ref` or `artifact://name/...` URI to the implementation subagent.
3. Implementation subagent calls `get_artifact` (or `resources/read`) to load the plan when needed.
