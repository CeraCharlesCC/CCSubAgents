package bootstrap

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type executeManagerStub struct {
	installVersion        string
	pinned                bool
	skipAttestationsCheck bool
	verbose               bool
	statusWriter          io.Writer
	setStatusWriterCalled bool
	promptInput           io.Reader
	promptOutput          io.Writer
	setPromptIOCalled     bool
	runCtx                context.Context
	runCommand            Command
	runScope              Scope
	runCalled             bool
	runErr                error
}

func (m *executeManagerStub) SetSkipAttestationsCheck(skip bool) {
	m.skipAttestationsCheck = skip
}

func (m *executeManagerStub) SetVerbose(verbose bool) {
	m.verbose = verbose
}

func (m *executeManagerStub) SetInstallVersion(version string) {
	m.installVersion = version
}

func (m *executeManagerStub) SetPinned(pinned bool) {
	m.pinned = pinned
}

func (m *executeManagerStub) SetStatusWriter(writer io.Writer) {
	m.setStatusWriterCalled = true
	m.statusWriter = writer
}

func (m *executeManagerStub) SetInstallPromptIO(input io.Reader, output io.Writer) {
	m.setPromptIOCalled = true
	m.promptInput = input
	m.promptOutput = output
}

func (m *executeManagerStub) Run(ctx context.Context, command Command, scope Scope) error {
	m.runCalled = true
	m.runCtx = ctx
	m.runCommand = command
	m.runScope = scope
	return m.runErr
}

func TestExecute_PropagatesStatusWriterAndPromptIO(t *testing.T) {
	stub := &executeManagerStub{runErr: errors.New("run failed")}
	originalFactory := newExecuteManager
	newExecuteManager = func() executeManager { return stub }
	defer func() { newExecuteManager = originalFactory }()

	var statusOut strings.Builder
	promptIn := strings.NewReader("1\n")
	var promptOut strings.Builder
	ctx := context.Background()

	err := Execute(ctx, ExecuteRequest{
		Command: CommandUpdate,
		Scope:   ScopeGlobal,
		Options: ExecuteOptions{
			InstallVersion:        "v1.2.3",
			Pinned:                true,
			SkipAttestationsCheck: true,
			Verbose:               true,
			StatusWriter:          &statusOut,
			PromptInput:           promptIn,
			PromptOutput:          &promptOut,
		},
	})
	if !errors.Is(err, stub.runErr) {
		t.Fatalf("expected run error %v, got %v", stub.runErr, err)
	}

	if !stub.skipAttestationsCheck {
		t.Fatalf("expected skip attestation option to propagate")
	}
	if stub.installVersion != "v1.2.3" {
		t.Fatalf("expected install version to propagate, got %q", stub.installVersion)
	}
	if !stub.pinned {
		t.Fatalf("expected pinned option to propagate")
	}
	if !stub.verbose {
		t.Fatalf("expected verbose option to propagate")
	}
	if !stub.setStatusWriterCalled {
		t.Fatalf("expected status writer to be set")
	}
	if stub.statusWriter != &statusOut {
		t.Fatalf("expected status writer %p, got %p", &statusOut, stub.statusWriter)
	}
	if !stub.setPromptIOCalled {
		t.Fatalf("expected prompt io to be set")
	}
	if stub.promptInput != promptIn {
		t.Fatalf("expected prompt input to be propagated")
	}
	if stub.promptOutput != &promptOut {
		t.Fatalf("expected prompt output to be propagated")
	}
	if !stub.runCalled {
		t.Fatalf("expected run to be called")
	}
	if stub.runCtx != ctx {
		t.Fatalf("expected run context to match execute context")
	}
	if stub.runCommand != CommandUpdate || stub.runScope != ScopeGlobal {
		t.Fatalf("expected run to be invoked with update/global, got %q/%q", stub.runCommand, stub.runScope)
	}
}

func TestExecute_DoesNotSetPromptIOWhenPromptStreamsNil(t *testing.T) {
	stub := &executeManagerStub{}
	originalFactory := newExecuteManager
	newExecuteManager = func() executeManager { return stub }
	defer func() { newExecuteManager = originalFactory }()

	err := Execute(context.Background(), ExecuteRequest{
		Command: CommandInstall,
		Scope:   ScopeLocal,
		Options: ExecuteOptions{
			SkipAttestationsCheck: false,
			Verbose:               false,
		},
	})
	if err != nil {
		t.Fatalf("expected execute to return nil when run succeeds, got %v", err)
	}

	if stub.setPromptIOCalled {
		t.Fatalf("expected prompt io not to be set when prompt streams are nil")
	}
	if stub.setStatusWriterCalled {
		t.Fatalf("expected status writer not to be set when status writer is nil")
	}
	if !stub.runCalled {
		t.Fatalf("expected run to be called")
	}
	if stub.runCommand != CommandInstall || stub.runScope != ScopeLocal {
		t.Fatalf("expected run to be invoked with install/local, got %q/%q", stub.runCommand, stub.runScope)
	}
}
