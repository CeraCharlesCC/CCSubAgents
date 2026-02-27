package daemonctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
)

type daemonHealthClient interface {
	Health(context.Context) error
}

var newDefaultHealthClient = func(stateDir string) (daemonHealthClient, error) {
	return daemonclient.NewDefaultClient(stateDir, os.Getenv)
}

func StartAndWait(ctx context.Context, stateDir, storeRoot string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	token, err := ensureToken(stateDir)
	if err != nil {
		return err
	}
	if err := startProcess(stateDir, storeRoot, token, stderr); err != nil {
		return err
	}
	client, err := daemonclient.NewDefaultClient(stateDir, os.Getenv)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := client.Health(ctx); err == nil {
			_, err = client.List(ctx, daemonclient.ListRequest{Workspace: daemonclient.WorkspaceSelector{WorkspaceID: "global"}, Limit: 1})
			if err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for daemon readiness")
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func WaitForStop(ctx context.Context, stateDir string, timeout time.Duration) error {
	client, err := newDefaultHealthClient(stateDir)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := client.Health(ctx)
		if err != nil && isExplicitStoppedHealthError(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("daemon stop verification failed: %w", err)
		}
		if time.Now().After(deadline) {
			return errors.New("daemon did not stop before timeout")
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func isExplicitStoppedHealthError(err error) bool {
	var remoteErr *daemonclient.RemoteError
	if !errors.As(err, &remoteErr) {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(remoteErr.Message))
	return strings.Contains(msg, "already stopped") || strings.Contains(msg, "already unavailable")
}

func startProcess(stateDir, storeRoot, token string, stderr io.Writer) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	daemonPath := filepath.Join(filepath.Dir(exePath), daemonBinaryName())
	cmd := exec.Command(daemonPath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(),
		"LOCAL_ARTIFACT_STORE_DIR="+storeRoot,
		"LOCAL_ARTIFACT_STATE_DIR="+stateDir,
		"LOCAL_ARTIFACT_DAEMON_TOKEN="+token,
	)
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "LOCAL_ARTIFACT_DAEMON_ADDR="+defaultDaemonAddr())
	} else {
		cmd.Env = append(cmd.Env, "LOCAL_ARTIFACT_DAEMON_SOCKET="+defaultDaemonSocket(stateDir))
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon %s: %w", daemonPath, err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

func ensureToken(stateDir string) (string, error) {
	tokenPath := filepath.Join(stateDir, "daemon", "daemon.token")
	if b, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(b))
		if token != "" {
			return token, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func daemonBinaryName() string {
	if runtime.GOOS == "windows" {
		return "ccsubagentsd.exe"
	}
	return "ccsubagentsd"
}

func defaultDaemonSocket(stateDir string) string {
	return filepath.Join(stateDir, "daemon", "ccsubagentsd.sock")
}

func defaultDaemonAddr() string {
	if addr := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_DAEMON_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:19131"
}
