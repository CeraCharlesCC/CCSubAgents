package bootstrap

import (
	"context"
	"io"
)

type executeManager interface {
	SetSkipAttestationsCheck(bool)
	SetVerbose(bool)
	SetStatusWriter(io.Writer)
	SetInstallPromptIO(io.Reader, io.Writer)
	Run(context.Context, Command, Scope) error
}

var newExecuteManager = func() executeManager {
	return NewManager()
}

type ExecuteRequest struct {
	Command Command
	Scope   Scope
	Options ExecuteOptions
}

type ExecuteOptions struct {
	SkipAttestationsCheck bool
	Verbose               bool
	StatusWriter          io.Writer
	PromptInput           io.Reader
	PromptOutput          io.Writer
}

func Execute(ctx context.Context, request ExecuteRequest) error {
	manager := newExecuteManager()
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
