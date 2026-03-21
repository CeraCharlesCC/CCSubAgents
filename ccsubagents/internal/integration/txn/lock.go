package txn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/files"
)

const staleLockMaxAge = 24 * time.Hour

var runtimeGOOS = runtime.GOOS

type lockFile struct {
	PID       int    `json:"pid"`
	CreatedAt string `json:"createdAt"`
}

func acquireLock(stateDir, scopeID string) (func(), error) {
	lockPath := filepath.Join(stateDir, "locks", scopeID+".lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), files.DefaultStateDirPerm); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	release, err := createLock(lockPath)
	if err == nil {
		return release, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("acquire %s lock: %w", scopeID, err)
	}

	recovered, recoverErr := recoverStaleLock(lockPath, time.Now().UTC())
	if recoverErr != nil {
		return nil, fmt.Errorf("recover stale %s lock: %w", scopeID, recoverErr)
	}
	if !recovered {
		return nil, fmt.Errorf("acquire %s lock: lock already held", scopeID)
	}

	release, err = createLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire %s lock after stale recovery: %w", scopeID, err)
	}
	return release, nil
}

func createLock(lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, files.DefaultStateFilePerm)
	if err != nil {
		return nil, err
	}
	payload, marshalErr := json.Marshal(lockFile{PID: os.Getpid(), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	if marshalErr != nil {
		closeIgnore(f)
		removeIfExists(lockPath)
		return nil, marshalErr
	}
	if _, writeErr := f.Write(append(payload, '\n')); writeErr != nil {
		closeIgnore(f)
		removeIfExists(lockPath)
		return nil, writeErr
	}
	return func() {
		closeIgnore(f)
		removeIfExists(lockPath)
	}, nil
}

func recoverStaleLock(lockPath string, now time.Time) (bool, error) {
	info, err := os.Stat(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}

	b, err := os.ReadFile(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}

	lock := lockFile{}
	if err := json.Unmarshal(b, &lock); err != nil {
		lock = lockFile{}
	}

	if lock.PID > 0 {
		if processExists(lock.PID) {
			if shouldTreatPIDLockAsStale(runtimeGOOS, createdAtForLock(lock, info), now) {
				if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return false, err
				}
				return true, nil
			}
			return false, nil
		}
		if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return true, nil
	}

	createdAt := createdAtForLock(lock, info)
	if now.Sub(createdAt) <= staleLockMaxAge {
		return false, nil
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

func createdAtForLock(lock lockFile, info os.FileInfo) time.Time {
	createdAt := info.ModTime().UTC()
	if parsed, parseErr := time.Parse(time.RFC3339, lock.CreatedAt); parseErr == nil {
		createdAt = parsed.UTC()
	}
	return createdAt
}

func shouldTreatPIDLockAsStale(goos string, createdAt, now time.Time) bool {
	if goos != "windows" {
		return false
	}
	return now.Sub(createdAt) > staleLockMaxAge
}
