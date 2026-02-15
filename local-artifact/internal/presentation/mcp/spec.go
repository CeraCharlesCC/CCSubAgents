package mcp

const ProtocolVersion = "2025-11-25"

const (
	serverName         = "local_artifact_store"
	serverTitle        = "Local Artifact Store"
	serverVersion      = "0.1.0"
	serverDescription  = "Completely local MCP server that lets agents save and retrieve named artifacts (text, files, images)."
	serverInstructions = "Use save_artifact_text or save_artifact_blob to persist an artifact under a unique name. Use get_artifact with name or ref to retrieve, delete_artifact to remove an artifact, and get_artifact_list to inspect current aliases."
)

const (
	toolArtifactSaveText = "save_artifact_text"
	toolArtifactSaveBlob = "save_artifact_blob"
	toolArtifactResolve  = "resolve_artifact"
	toolArtifactGet      = "get_artifact"
	toolArtifactList     = "get_artifact_list"
	toolArtifactDelete   = "delete_artifact"
)

const (
	modeAuto     = "auto"
	modeText     = "text"
	modeResource = "resource"
	modeImage    = "image"
	modeMeta     = "meta"
)

type toolDef struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations,omitempty"`
}

func initializeResponse() map[string]any {
	return map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
			"resources": map[string]any{
				"subscribe":   false,
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":        serverName,
			"title":       serverTitle,
			"version":     serverVersion,
			"description": serverDescription,
		},
		"instructions": serverInstructions,
	}
}

func toolDefinitions() []toolDef {
	return []toolDef{
		{
			Name:        toolArtifactSaveText,
			Title:       "Save text artifact",
			Description: "Save UTF-8 text under a name and return a stable ref and artifact:// URIs.",
			InputSchema: objectSchema(
				map[string]any{
					"name":     stringProp("Artifact name/alias (e.g. plan/task-123)."),
					"text":     stringProp("Text content to save."),
					"mimeType": stringProp("Optional MIME type. Defaults to text/plain; charset=utf-8."),
				},
				"name", "text",
			),
			OutputSchema: saveOutputSchema(),
			Annotations:  readOnlyHint(false),
		},
		{
			Name:        toolArtifactSaveBlob,
			Title:       "Save file/image artifact",
			Description: "Save a binary blob (e.g., a text file or image) under a name. Data is base64 encoded.",
			InputSchema: objectSchema(
				map[string]any{
					"name":       stringProp("Artifact name/alias."),
					"dataBase64": stringProp("Base64-encoded bytes."),
					"mimeType":   stringProp("MIME type (e.g., image/png, application/pdf, text/markdown)."),
					"filename":   stringProp("Optional original filename."),
				},
				"name", "dataBase64", "mimeType",
			),
			OutputSchema: saveOutputSchema(),
			Annotations:  readOnlyHint(false),
		},
		{
			Name:        toolArtifactResolve,
			Title:       "Resolve name to ref",
			Description: "Given a name, return the latest ref and URIs without loading the artifact body.",
			InputSchema: objectSchema(
				map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"name",
			),
			OutputSchema: resolveOutputSchema(),
			Annotations:  readOnlyHint(true),
		},
		{
			Name:        toolArtifactGet,
			Title:       "Get artifact",
			Description: "Fetch an artifact by ref or name. For binary, returns embedded resource (base64) unless mode=image.",
			InputSchema: objectSchema(
				map[string]any{
					"ref":  map[string]any{"type": "string"},
					"name": map[string]any{"type": "string"},
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{modeAuto, modeText, modeResource, modeImage, modeMeta},
						"description": "auto=text for text/*, else resource",
					},
				},
			),
			OutputSchema: map[string]any{"type": "object"},
			Annotations:  readOnlyHint(true),
		},
		{
			Name:        toolArtifactList,
			Title:       "List artifacts",
			Description: "List latest artifacts by name prefix.",
			InputSchema: objectSchema(
				map[string]any{
					"prefix": stringProp("Optional name prefix filter."),
					"limit":  map[string]any{"type": "integer", "description": "Max results (default 200)."},
				},
			),
			OutputSchema: objectSchema(
				map[string]any{
					"items": map[string]any{
						"type":  "array",
						"items": saveOutputSchema(),
					},
				},
				"items",
			),
			Annotations: readOnlyHint(true),
		},
		{
			Name:        toolArtifactDelete,
			Title:       "Delete artifact",
			Description: "Delete an artifact by name or ref. If ref is provided, all names pointing to that ref are removed.",
			InputSchema: objectSchema(
				map[string]any{
					"ref":  stringProp("Artifact ref to delete."),
					"name": stringProp("Artifact alias/name to delete."),
				},
			),
			OutputSchema: deleteOutputSchema(),
			Annotations:  readOnlyHint(false),
		},
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProp(description string) map[string]any {
	prop := map[string]any{"type": "string"}
	if description != "" {
		prop["description"] = description
	}
	return prop
}

func saveOutputSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":      map[string]any{"type": "string"},
			"ref":       map[string]any{"type": "string"},
			"kind":      map[string]any{"type": "string"},
			"mimeType":  map[string]any{"type": "string"},
			"filename":  map[string]any{"type": "string"},
			"sizeBytes": map[string]any{"type": "integer"},
			"sha256":    map[string]any{"type": "string"},
			"createdAt": map[string]any{"type": "string"},
			"uriByName": map[string]any{"type": "string"},
			"uriByRef":  map[string]any{"type": "string"},
			"prevRef":   map[string]any{"type": "string"},
		},
		"name", "ref", "kind", "mimeType", "sizeBytes", "sha256", "createdAt", "uriByName", "uriByRef",
	)
}

func resolveOutputSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":      map[string]any{"type": "string"},
			"ref":       map[string]any{"type": "string"},
			"uriByName": map[string]any{"type": "string"},
			"uriByRef":  map[string]any{"type": "string"},
		},
		"name", "ref", "uriByName", "uriByRef",
	)
}

func deleteOutputSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":      map[string]any{"type": "string"},
			"ref":       map[string]any{"type": "string"},
			"deleted":   map[string]any{"type": "boolean"},
			"uriByName": map[string]any{"type": "string"},
			"uriByRef":  map[string]any{"type": "string"},
		},
		"ref", "deleted", "uriByRef",
	)
}

func readOnlyHint(readOnly bool) map[string]any {
	return map[string]any{"readOnlyHint": readOnly}
}
