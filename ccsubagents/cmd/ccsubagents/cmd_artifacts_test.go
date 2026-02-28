package main

import (
	"bytes"
	"testing"
)

func TestLooksLikeRef_StrictPattern(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid ref", value: "20260227T120000Z-aaaaaaaaaaaaaaaa", want: true},
		{name: "valid ref with millis", value: "20260227T120000.123Z-aaaaaaaaaaaaaaaa", want: true},
		{name: "artifact name with dashes", value: "plan/task-003-fix-1", want: false},
		{name: "uppercase hex not allowed", value: "20260227T120000Z-AAAAAAAAAAAAAAAA", want: false},
		{name: "missing timestamp", value: "just-a-name", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeRef(tc.value); got != tc.want {
				t.Fatalf("looksLikeRef(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestRunArtifacts_NoArgs_UsageExit2(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runArtifacts(nil, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runArtifacts exit=%d, want=2", code)
	}
	if got := stderr.String(); got != "Usage: ccsubagents artifacts <ls|get|put|openwebui>\n" {
		t.Fatalf("stderr mismatch: got=%q", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

func TestRunArtifacts_UnknownSubcommand_Exit2(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runArtifacts([]string{"wat"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runArtifacts exit=%d, want=2", code)
	}
	if got := stderr.String(); got != "unknown artifacts subcommand \"wat\"\n" {
		t.Fatalf("stderr mismatch: got=%q", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

func TestRunArtifactsGet_WrongArgCount_ShowsUsageExit2(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runArtifacts([]string{"get"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runArtifacts exit=%d, want=2", code)
	}
	if got := stderr.String(); got != "Usage: ccsubagents artifacts get <name|ref> [--out PATH|-]\n" {
		t.Fatalf("stderr mismatch: got=%q", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

func TestRunArtifactsPut_WrongArgCount_ShowsUsageExit2(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runArtifacts([]string{"put", "only-name"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runArtifacts exit=%d, want=2", code)
	}
	if got := stderr.String(); got != "Usage: ccsubagents artifacts put <name> <path|-> [flags]\n" {
		t.Fatalf("stderr mismatch: got=%q", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

func TestRunArtifactsPut_EmptyName_IsUsageErrorExit2(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runArtifacts([]string{"put", "", "-"}, bytes.NewBufferString("payload"), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runArtifacts exit=%d, want=2", code)
	}
	if got := stderr.String(); got != "artifact name is required\n" {
		t.Fatalf("stderr mismatch: got=%q", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}
