package daemonctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	processExistsFn = processExists
	sendGracefulFn  = sendGraceful
	sendForceFn     = sendForce
	stopSleep       = time.Sleep
	stopNow         = time.Now
)

const (
	gracefulStopTimeout = 2 * time.Second
	forceStopTimeout    = 2 * time.Second
	stopPollInterval    = 120 * time.Millisecond
)

func StopRegisteredProcesses(ctx context.Context, stateDir string, roles []string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	_ = stderr

	var errs []error
	for _, role := range roles {
		if err := ctx.Err(); err != nil {
			return err
		}
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		errs = append(errs, stopRegisteredRole(ctx, stateDir, role)...)
	}
	return errors.Join(errs...)
}

func stopRegisteredRole(ctx context.Context, stateDir, role string) []error {
	pids, pidFilePaths, err := listRolePIDs(stateDir, role)
	if err != nil {
		return []error{fmt.Errorf("list registered %s processes: %w", role, err)}
	}

	validPIDFiles := make(map[string]struct{}, len(pidFilePaths))
	for _, path := range pidFilePaths {
		validPIDFiles[path] = struct{}{}
	}

	var errs []error
	invalidPaths, err := listInvalidPIDRegistryFiles(registryRoleDir(stateDir, role), validPIDFiles)
	if err != nil {
		errs = append(errs, fmt.Errorf("list invalid %s registry entries: %w", role, err))
	}
	for _, invalidPath := range invalidPaths {
		if removeErr := removePIDFile(invalidPath); removeErr != nil {
			errs = append(errs, fmt.Errorf("remove invalid %s pid file %s: %w", role, invalidPath, removeErr))
		}
	}

	for i, pid := range pids {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			return errs
		}
		pidFilePath := pidFilePaths[i]
		if !processExistsFn(pid) {
			if removeErr := removePIDFile(pidFilePath); removeErr != nil {
				errs = append(errs, fmt.Errorf("remove stale %s pid file %s: %w", role, pidFilePath, removeErr))
			}
			continue
		}

		if stopErr := stopRegisteredPID(ctx, pid); stopErr != nil {
			errs = append(errs, fmt.Errorf("stop %s pid %d: %w", role, pid, stopErr))
			continue
		}
		if removeErr := removePIDFile(pidFilePath); removeErr != nil {
			errs = append(errs, fmt.Errorf("remove %s pid file %s after stop: %w", role, pidFilePath, removeErr))
		}
	}

	return errs
}

func listInvalidPIDRegistryFiles(roleDir string, validPIDFiles map[string]struct{}) ([]string, error) {
	entries, err := os.ReadDir(roleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	invalid := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(roleDir, entry.Name())
		if _, ok := validPIDFiles[path]; ok {
			continue
		}
		invalid = append(invalid, path)
	}
	return invalid, nil
}

func stopRegisteredPID(ctx context.Context, pid int) error {
	var gracefulErr error
	if err := sendGracefulFn(pid); err != nil && !isProcessGoneError(err) {
		if !processExistsFn(pid) {
			return nil
		}
		gracefulErr = err
	}

	exited, err := waitForProcessExit(ctx, pid, gracefulStopTimeout)
	if err != nil {
		return err
	}
	if exited {
		return nil
	}

	if err := sendForceFn(pid); err != nil && !isProcessGoneError(err) {
		if !processExistsFn(pid) {
			return nil
		}
		if gracefulErr != nil {
			return fmt.Errorf("force stop after graceful error (%v): %w", gracefulErr, err)
		}
		return fmt.Errorf("force stop: %w", err)
	}

	exited, err = waitForProcessExit(ctx, pid, forceStopTimeout)
	if err != nil {
		return err
	}
	if exited {
		return nil
	}

	if gracefulErr != nil {
		return fmt.Errorf("process did not exit after force stop (graceful stop error: %w)", gracefulErr)
	}
	return errors.New("process did not exit after force stop")
}

func waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		return !processExistsFn(pid), nil
	}
	deadline := stopNow().Add(timeout)
	for {
		if !processExistsFn(pid) {
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !stopNow().Before(deadline) {
			return false, nil
		}
		stopSleep(stopPollInterval)
	}
}

func removePIDFile(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isProcessGoneError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrProcessDone) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "no such process") ||
		strings.Contains(msg, "already finished") ||
		strings.Contains(msg, "not found")
}
