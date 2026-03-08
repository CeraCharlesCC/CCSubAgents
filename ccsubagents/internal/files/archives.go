package files

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultStateDirPerm   = 0o755
	DefaultStateFilePerm  = 0o644
	DefaultBinaryFilePerm = 0o755
	maxBundleBinarySize   = 512 << 20 // 512 MiB
	maxAgentsFileSize     = 32 << 20  // 32 MiB
	maxAgentsArchiveSize  = 256 << 20 // 256 MiB
)

func InstallBinary(srcPath, dstPath string, perm os.FileMode) error {
	return InstallBinaryWithinBase(srcPath, dstPath, filepath.Dir(dstPath), perm)
}

func InstallBinaryWithinBase(srcPath, dstPath, base string, perm os.FileMode) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source binary %s: %w", srcPath, err)
	}
	if err := RejectSymlinkPathWithinBase(filepath.Dir(dstPath), base); err != nil {
		return err
	}
	if err := RejectSymlinkPathWithinBase(dstPath, base); err != nil {
		return err
	}
	if err := os.WriteFile(dstPath, data, perm); err != nil {
		return err
	}
	return nil
}

func ExtractBundleBinaries(zipPath, destDir string, names []string, perm os.FileMode) (map[string]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer r.Close()

	expected := map[string]struct{}{}
	for _, name := range names {
		expected[name] = struct{}{}
	}

	extracted := map[string]string{}
	for _, file := range r.File {
		if file.FileInfo().IsDir() {
			continue
		}

		clean, err := cleanZipPath(file.Name)
		if err != nil {
			return nil, err
		}
		if clean == "" {
			continue
		}

		baseName := path.Base(clean)
		if _, ok := expected[baseName]; !ok {
			continue
		}
		if _, exists := extracted[baseName]; exists {
			return nil, fmt.Errorf("archive contains duplicate %q", baseName)
		}

		destPath := filepath.Join(destDir, baseName)
		if _, err := writeZipEntryWithinBase(file, destPath, destDir, perm, maxBundleBinarySize); err != nil {
			return nil, err
		}
		extracted[baseName] = destPath
	}

	missing := []string{}
	for _, name := range names {
		if _, ok := extracted[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("archive missing required file(s): %s", strings.Join(missing, ", "))
	}

	return extracted, nil
}

func ExtractAgentsArchiveWithHook(zipPath, destDir string, beforeWrite func(string) error, stateDirPerm, stateFilePerm os.FileMode) (filesOut []string, dirsOut []string, retErr error) {
	return extractAgentsArchiveWithHookAndLimits(zipPath, destDir, beforeWrite, stateDirPerm, stateFilePerm, maxAgentsFileSize, maxAgentsArchiveSize)
}

func extractAgentsArchiveWithHookAndLimits(zipPath, destDir string, beforeWrite func(string) error, stateDirPerm, stateFilePerm os.FileMode, maxFileSize, maxArchiveSize int64) (filesOut []string, dirsOut []string, retErr error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open archive: %w", err)
	}
	defer r.Close()

	stripAgentsPrefix, err := shouldStripAgentsPrefix(r.File)
	if err != nil {
		return nil, nil, err
	}

	files := []string{}
	dirs := []string{}
	writtenFiles := []string{}
	var totalWritten int64
	defer func() {
		if retErr == nil {
			return
		}
		for _, filePath := range writtenFiles {
			_ = os.Remove(filePath)
		}
	}()

	for _, file := range r.File {
		clean, err := cleanZipPath(file.Name)
		if err != nil {
			return nil, nil, err
		}
		if clean == "" {
			continue
		}

		if stripAgentsPrefix {
			if clean == "agents" {
				continue
			}
			clean = strings.TrimPrefix(clean, "agents/")
			if strings.TrimSpace(clean) == "" || clean == "." {
				continue
			}
		}

		destPath := filepath.Join(destDir, filepath.FromSlash(clean))
		destPath = filepath.Clean(destPath)
		if !IsPathWithinDir(destPath, destDir) {
			return nil, nil, fmt.Errorf("archive path escapes destination: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := RejectSymlinkPathWithinBase(destPath, destDir); err != nil {
				return nil, nil, err
			}
			if err := os.MkdirAll(destPath, stateDirPerm); err != nil {
				return nil, nil, fmt.Errorf("create directory %s: %w", destPath, err)
			}
			dirs = append(dirs, destPath)
			continue
		}

		parent := filepath.Dir(destPath)
		if err := RejectSymlinkPathWithinBase(parent, destDir); err != nil {
			return nil, nil, err
		}
		if err := os.MkdirAll(parent, stateDirPerm); err != nil {
			return nil, nil, fmt.Errorf("create directory %s: %w", parent, err)
		}
		dirs = append(dirs, parent)

		mode := file.FileInfo().Mode().Perm()
		if mode == 0 {
			mode = stateFilePerm
		}
		if beforeWrite != nil {
			if err := beforeWrite(destPath); err != nil {
				return nil, nil, err
			}
		}
		remainingArchiveBudget := maxArchiveSize - totalWritten
		if remainingArchiveBudget <= 0 {
			return nil, nil, fmt.Errorf("archive exceeds maximum total extracted size of %d bytes", maxArchiveSize)
		}
		entryLimit := minInt64(maxFileSize, remainingArchiveBudget)
		written, err := writeZipEntryWithinBase(file, destPath, destDir, mode, entryLimit)
		if err != nil {
			return nil, nil, err
		}
		totalWritten += written
		writtenFiles = append(writtenFiles, destPath)
		files = append(files, destPath)
	}

	return UniqueSorted(files), UniqueSorted(dirs), nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func writeZipEntry(file *zip.File, destPath string, perm os.FileMode, maxSize int64) (written int64, retErr error) {
	return writeZipEntryWithinBase(file, destPath, filepath.Dir(destPath), perm, maxSize)
}

func writeZipEntryWithinBase(file *zip.File, destPath, base string, perm os.FileMode, maxSize int64) (written int64, retErr error) {
	if err := RejectSymlinkPathWithinBase(filepath.Dir(destPath), base); err != nil {
		return 0, err
	}
	if err := RejectSymlinkPathWithinBase(destPath, base); err != nil {
		return 0, err
	}

	rc, err := file.Open()
	if err != nil {
		return 0, fmt.Errorf("open archive file %s: %w", file.Name, err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close archive file %s: %w", file.Name, closeErr)
		}
	}()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return 0, fmt.Errorf("create extracted file %s: %w", destPath, err)
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close extracted file %s: %w", destPath, closeErr)
		}
		if retErr != nil {
			_ = os.Remove(destPath)
		}
	}()

	written, err = io.CopyN(out, rc, maxSize+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return written, fmt.Errorf("read archive file %s: %w", file.Name, err)
	}
	if written > maxSize {
		return written, fmt.Errorf("archive file %s exceeds maximum size of %d bytes", file.Name, maxSize)
	}

	return written, nil
}

func shouldStripAgentsPrefix(files []*zip.File) (bool, error) {
	seen := false
	for _, file := range files {
		clean, err := cleanZipPath(file.Name)
		if err != nil {
			return false, err
		}
		if clean == "" {
			continue
		}
		seen = true
		if clean == "agents" || strings.HasPrefix(clean, "agents/") {
			continue
		}
		return false, nil
	}
	return seen, nil
}

func cleanZipPath(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", nil
	}
	normalized := strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean(normalized)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || hasWindowsDrivePrefix(clean) {
		return "", fmt.Errorf("unsafe archive path: %s", raw)
	}
	return clean, nil
}

func hasWindowsDrivePrefix(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func RemoveStaleAgentFilesWithHook(oldFiles, newFiles []string, agentsDir string, beforeRemove func(string) error) error {
	newSet := map[string]struct{}{}
	for _, path := range newFiles {
		newSet[filepath.Clean(path)] = struct{}{}
	}

	for _, path := range oldFiles {
		clean := filepath.Clean(path)
		if !IsPathWithinDir(clean, agentsDir) {
			continue
		}
		if _, keep := newSet[clean]; keep {
			continue
		}
		if beforeRemove != nil {
			if err := beforeRemove(clean); err != nil {
				return err
			}
		}
		if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale managed agent file %s: %w", clean, err)
		}
	}
	return nil
}
