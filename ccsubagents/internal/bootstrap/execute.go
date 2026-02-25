package bootstrap

import (
	"context"
	"io"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/installer"
)

type executeManager interface {
	SetSkipAttestationsCheck(bool)
	SetVerbose(bool)
	SetStatusWriter(io.Writer)
	SetInstallPromptIO(io.Reader, io.Writer)
	SetInstallVersion(string)
	SetPinned(bool)
	Run(context.Context, Command, Scope) error
}

var newExecuteManager = func() executeManager {
	return installer.NewRunner()
}

type ExecuteRequest struct {
	Command Command
	Scope   Scope
	Options ExecuteOptions
}

type ExecuteOptions struct {
	InstallVersion        string
	Pinned                bool
	SkipAttestationsCheck bool
	Verbose               bool
	StatusWriter          io.Writer
	PromptInput           io.Reader
	PromptOutput          io.Writer
}

func Execute(ctx context.Context, request ExecuteRequest) error {
	manager := newExecuteManager()
	manager.SetInstallVersion(request.Options.InstallVersion)
	manager.SetPinned(request.Options.Pinned)
	manager.SetSkipAttestationsCheck(request.Options.SkipAttestationsCheck)
	manager.SetVerbose(request.Options.Verbose)

	if request.Options.StatusWriter != nil {
		manager.SetStatusWriter(request.Options.StatusWriter)
	}
	if request.Options.PromptInput != nil || request.Options.PromptOutput != nil {
		manager.SetInstallPromptIO(request.Options.PromptInput, request.Options.PromptOutput)
	}

	return manager.Run(ctx, request.Command, request.Scope)
}
