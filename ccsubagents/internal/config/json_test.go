package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadJSONFile_AcceptsJSONCCommentsAndTrailingCommas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{
  // top-level comment
  "github.copilot.chat.responsesApiReasoningEffort": "xhigh",
  "chat.customAgentInSubagent.enabled": true, // inline comment
  "nested": {
    "value": 1,
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	root, err := readJSONFile(path)
	if err != nil {
		t.Fatalf("read json file: %v", err)
	}

	if root["github.copilot.chat.responsesApiReasoningEffort"] != "xhigh" {
		t.Fatalf("expected string key preserved, got %#v", root["github.copilot.chat.responsesApiReasoningEffort"])
	}
	if root["chat.customAgentInSubagent.enabled"] != true {
		t.Fatalf("expected boolean key preserved, got %#v", root["chat.customAgentInSubagent.enabled"])
	}
	nested, ok := root["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested object, got %#v", root["nested"])
	}
	if nested["value"] != float64(1) {
		t.Fatalf("expected nested value preserved, got %#v", nested["value"])
	}
}

func TestApplySettingsEdit_AcceptsVSCodeSettingsWithTrailingComma(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{
  "github.copilot.chat.anthropic.thinking.budgetTokens": 31999,
  "github.copilot.chat.responsesApiReasoningEffort": "xhigh",
  "chat.customAgentInSubagent.enabled": true,
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := ApplySettingsEdit(path, "~/.local/share/ccsubagents/agents", nil, 0o644); err != nil {
		t.Fatalf("apply settings edit: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated settings: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("decode updated settings: %v", err)
	}
	locations, ok := root[SettingsAgentPathKey].(map[string]any)
	if !ok {
		t.Fatalf("expected %s object, got %#v", SettingsAgentPathKey, root[SettingsAgentPathKey])
	}
	if locations["~/.local/share/ccsubagents/agents"] != true {
		t.Fatalf("expected managed agent path to be added, got %#v", locations)
	}
}
