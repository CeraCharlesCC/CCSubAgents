package daemonctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

var (
	processExistsFn          = processExists
	processIdentityMatchesFn = processIdentityMatches
	sendGracefulFn           = sendGraceful
	sendForceFn              = sendForce
	stopSleep                = time.Sleep
	stopNow                  = time.Now
)

const (
	gracefulStopTimeout = 2 * time.Second
	forceStopTimeout    = 2 * time.Second
	stopPollInterval    = 120 * time.Millisecond
)

func StopRegisteredProcesses(ctx context.Context, stateDir string, roles []string) error {
	var errs []error
	for _, role := range roles {
		if err := ctx.Err(); err != nil {
			return err
		}

		trimmedRole := strings.TrimSpace(role)
		if trimmedRole == "" {
			continue
		}

		safeRole, ok := sanitizeRegistryRole(trimmedRole)
		if !ok {
			errs = append(errs, fmt.Errorf("invalid role %q", trimmedRole))
			continue
		}
		errs = append(errs, stopRegisteredRole(ctx, stateDir, safeRole)...)
	}
	return errors.Join(errs...)
}

func stopRegisteredRole(ctx context.Context, stateDir, role string) []error {
	listing, err := listRolePIDs(stateDir, role)
	if err != nil {
		return []error{fmt.Errorf("list registered %s processes: %w", role, err)}
	}

	errs := append([]error(nil), listing.issues...)
	for _, item := range listing.registered {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			return errs
		}
		pid := item.pid
		pidFilePath := item.pidFilePath
		if !processExistsFn(pid) {
			if removeErr := removePIDFile(pidFilePath); removeErr != nil {
				errs = append(errs, fmt.Errorf("remove stale %s pid file %s: %w", role, pidFilePath, removeErr))
			}
			continue
		}
		matches, matchErr := processIdentityMatchesFn(pid, item.startID)
		if matchErr != nil {
			// The process can exit between liveness and identity checks.
			if !processExistsFn(pid) {
				if removeErr := removePIDFile(pidFilePath); removeErr != nil {
					errs = append(errs, fmt.Errorf("remove stale %s pid file %s: %w", role, pidFilePath, removeErr))
				}
				continue
			}
			errs = append(errs, fmt.Errorf("resolve %s pid %d identity: %w", role, pid, matchErr))
			continue
		}
		if !matches {
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

func processIdentityMatches(pid int, expectedStartID string) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid pid %d", pid)
	}
	expectedStartID = strings.TrimSpace(expectedStartID)
	if expectedStartID == "" {
		return false, errors.New("expected start id is empty")
	}
	actualStartID, err := processStartID(pid)
	if err != nil {
		return false, err
	}
	return actualStartID == expectedStartID, nil
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
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "parameter is incorrect")
}
