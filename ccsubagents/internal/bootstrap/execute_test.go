package bootstrap

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type executeManagerRecorder struct {
	got                ExecuteOptions
	statusWriterCalled bool
	promptIOCalled     bool
	runCalled          bool
	runCtx             context.Context
	runCommand         Command
	runScope           Scope
	runErr             error
}

func (m *executeManagerRecorder) SetSkipAttestationsCheck(skip bool) {
	m.got.SkipAttestationsCheck = skip
}
func (m *executeManagerRecorder) SetVerbose(verbose bool)          { m.got.Verbose = verbose }
func (m *executeManagerRecorder) SetInstallVersion(version string) { m.got.InstallVersion = version }
func (m *executeManagerRecorder) SetPinned(pinned bool)            { m.got.Pinned = pinned }
func (m *executeManagerRecorder) SetStatusWriter(writer io.Writer) {
	m.statusWriterCalled, m.got.StatusWriter = true, writer
}
func (m *executeManagerRecorder) SetInstallPromptIO(input io.Reader, output io.Writer) {
	m.promptIOCalled, m.got.PromptInput, m.got.PromptOutput = true, input, output
}
func (m *executeManagerRecorder) Run(ctx context.Context, command Command, scope Scope) error {
	m.runCalled, m.runCtx, m.runCommand, m.runScope = true, ctx, command, scope
	return m.runErr
}

func useExecuteManager(t *testing.T, stub executeManager) {
	t.Helper()
	original := newExecuteManager
	newExecuteManager = func() executeManager { return stub }
	t.Cleanup(func() { newExecuteManager = original })
}

func TestExecute(t *testing.T) {
	runErr := errors.New("run failed")
	statusOut, promptOut := io.Discard, io.Discard
	promptIn := strings.NewReader("1\n")

	tests := []struct {
		request          ExecuteRequest
		runErr           error
		wantErr          error
		wantStatusWriter bool
		wantPrompt       bool
	}{
		{
			request: ExecuteRequest{
				Command: CommandUpdate,
				Scope:   ScopeGlobal,
				Options: ExecuteOptions{
					InstallVersion:        "v1.2.3",
					Pinned:                true,
					SkipAttestationsCheck: true,
					Verbose:               true,
					StatusWriter:          statusOut,
					PromptInput:           promptIn,
					PromptOutput:          promptOut,
				},
			},
			runErr:           runErr,
			wantErr:          runErr,
			wantStatusWriter: true,
			wantPrompt:       true,
		},
		{request: ExecuteRequest{Command: CommandInstall, Scope: ScopeLocal}},
	}

	for i, tc := range tests {
		stub := &executeManagerRecorder{runErr: tc.runErr}
		useExecuteManager(t, stub)

		ctx := context.Background()
		err := Execute(ctx, tc.request)
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("case %d: expected error %v, got %v", i, tc.wantErr, err)
		}
		if stub.got != tc.request.Options {
			t.Fatalf("case %d: unexpected options: got %#v want %#v", i, stub.got, tc.request.Options)
		}
		if stub.statusWriterCalled != tc.wantStatusWriter {
			t.Fatalf("case %d: status writer call mismatch: got %v want %v", i, stub.statusWriterCalled, tc.wantStatusWriter)
		}
		if stub.promptIOCalled != tc.wantPrompt {
			t.Fatalf("case %d: prompt IO call mismatch: got %v want %v", i, stub.promptIOCalled, tc.wantPrompt)
		}
		if !stub.runCalled {
			t.Fatalf("case %d: expected run to be called", i)
		}
		if stub.runCtx != ctx || stub.runCommand != tc.request.Command || stub.runScope != tc.request.Scope {
			t.Fatalf("case %d: run args mismatch: got %v/%q/%q want %v/%q/%q", i, stub.runCtx, stub.runCommand, stub.runScope, ctx, tc.request.Command, tc.request.Scope)
		}
	}
}
