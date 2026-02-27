package daemonctl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
)

type fakeHealthClient struct {
	err error
}

func (f fakeHealthClient) Health(context.Context) error {
	return f.err
}

func TestWaitForStop_AcceptsExplicitAlreadyStoppedSignal(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "daemon already stopped"}}, nil
	}

	if err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond); err != nil {
		t.Fatalf("expected already-stopped success, got %v", err)
	}
}

func TestWaitForStop_RejectsNonStoppedServiceUnavailable(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: no such file or directory"}}, nil
	}

	err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected non-stopped service unavailable failure")
	}
	if !strings.Contains(err.Error(), "stop verification failed") {
		t.Fatalf("expected verification failure context, got %v", err)
	}
}

func TestWaitForStop_RejectsUnauthorizedHealthError(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeUnauthorized, Message: "missing or invalid token"}}, nil
	}

	err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected unauthorized health failure")
	}
	if !strings.Contains(err.Error(), "stop verification failed") {
		t.Fatalf("expected verification failure context, got %v", err)
	}
}

func TestWaitForStop_RejectsTransientHealthError(t *testing.T) {
	original := newDefaultHealthClient
	t.Cleanup(func() { newDefaultHealthClient = original })

	newDefaultHealthClient = func(string) (daemonHealthClient, error) {
		return fakeHealthClient{err: &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "gateway timeout"}}, nil
	}

	err := WaitForStop(context.Background(), t.TempDir(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected transient health failure")
	}
	var remoteErr *daemonclient.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != daemonclient.CodeInternal {
		t.Fatalf("expected wrapped internal remote error, got %v", err)
	}
}
