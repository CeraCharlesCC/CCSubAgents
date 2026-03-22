package mcp

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestArtifactTools_SuccessContentKeepsGetArtifactResourceLinks(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	saveTextResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
		"name": "plan/content-save-text",
		"text": "hello\n",
	}))
	requireContentTextEq(t, saveTextResp, "saved")
	requireNoContentType(t, saveTextResp, "resource_link")

	saveBlobResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveBlob, map[string]any{
		"name":       "plan/content-save-blob",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0x02}),
		"mimeType":   "application/octet-stream",
	}))
	requireContentTextEq(t, saveBlobResp, "saved")
	requireNoContentType(t, saveBlobResp, "resource_link")

	getTextResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactGet, map[string]any{
		"name": "plan/content-save-text",
		"mode": modeText,
	}))
	requireContentTextEq(t, getTextResp, "hello\n")
	getTextLink := requireHasContentType(t, getTextResp, "resource_link")
	if getTextLink["uri"] == "" {
		t.Fatalf("expected text-mode get resource_link uri, got %+v", getTextLink)
	}
	if len(getTextResp.Content) != 2 {
		t.Fatalf("expected text-mode get to return payload content plus resource_link, got %+v", getTextResp.Content)
	}

	getResourceResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactGet, map[string]any{
		"name": "plan/content-save-blob",
		"mode": modeResource,
	}))
	getResourceLink := requireHasContentType(t, getResourceResp, "resource_link")
	if getResourceLink["uri"] == "" {
		t.Fatalf("expected resource-mode get resource_link uri, got %+v", getResourceLink)
	}
	if len(getResourceResp.Content) != 2 {
		t.Fatalf("expected resource-mode get to return embedded resource plus resource_link, got %+v", getResourceResp.Content)
	}
	firstResource, ok := getResourceResp.Content[0].(map[string]any)
	if !ok || firstResource["type"] != "resource" {
		t.Fatalf("expected resource-mode get to return resource content, got %+v", getResourceResp.Content)
	}

	editResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactEditText, map[string]any{
		"operation": "append",
		"artifact":  map[string]any{"name": "plan/content-save-text"},
		"text":      "world\n",
	}))
	requireContentTextEq(t, editResp, "appended")
	requireNoContentType(t, editResp, "resource_link")
}

func TestHandleToolsCall_SchemaValidationRejectsGetAndDeleteSelectorOneOfViolations(t *testing.T) {
	s := newDaemonBackedServer(t)
	s.setInitialized(true)
	ctx := context.Background()

	testCases := []struct {
		name     string
		toolName string
		args     map[string]any
	}{
		{
			name:     "get requires name or ref",
			toolName: toolArtifactGet,
			args:     map[string]any{},
		},
		{
			name:     "get rejects both name and ref",
			toolName: toolArtifactGet,
			args:     map[string]any{"name": "plan/x", "ref": "20260216T101019Z-cccccccccccccccc"},
		},
		{
			name:     "delete requires name or ref",
			toolName: toolArtifactDelete,
			args:     map[string]any{},
		},
		{
			name:     "delete rejects both name and ref",
			toolName: toolArtifactDelete,
			args:     map[string]any{"name": "plan/x", "ref": "20260216T101019Z-cccccccccccccccc"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := requireToolErr(t, callToolsCall(t, s, ctx, tc.toolName, tc.args))
			if msg := firstContentText(resp); !strings.HasPrefix(msg, "Invalid arguments:") {
				t.Fatalf("unexpected schema validation message: %q", msg)
			}
		})
	}
}

func TestToolsList_ExposesGetAndDeleteSelectorOneOf(t *testing.T) {
	toolsResp, rpcErr := newDaemonBackedServer(t).handleToolsList(nil)
	if rpcErr != nil {
		t.Fatalf("tools/list error: %+v", rpcErr)
	}
	tools := requireToolDefs(t, requireMap(t, toolsResp, "tools/list response")["tools"])

	lookup := func(name string) toolDef {
		t.Helper()
		for _, tool := range tools {
			if tool.Name == name {
				return tool
			}
		}
		t.Fatalf("expected tools/list to include %q", name)
		return toolDef{}
	}

	for _, toolName := range []string{toolArtifactGet, toolArtifactDelete} {
		tool := lookup(toolName)
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("expected strict %s input schema, got %+v", toolName, tool.InputSchema)
		}
		oneOf, ok := tool.InputSchema["oneOf"].([]map[string]any)
		if !ok || len(oneOf) != 2 {
			t.Fatalf("expected %s selector schema oneOf with two selectors, got %+v", toolName, tool.InputSchema["oneOf"])
		}
	}
}
