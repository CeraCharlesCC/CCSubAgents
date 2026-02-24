package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type bundleBinarySpec struct {
	logicalName string
	archiveName string
	installName string
}

func (m *Manager) bundleBinarySpecs() ([]bundleBinarySpec, error) {
	goos := m.currentGOOS()
	goarch := m.currentGOARCH()

	switch goos {
	case "linux":
		if goarch != "amd64" && goarch != "arm64" {
			return nil, fmt.Errorf("unsupported platform %s/%s (supported linux arches: amd64, arm64)", goos, goarch)
		}
	case "windows":
		if goarch != "amd64" {
			return nil, fmt.Errorf("unsupported platform %s/%s (supported windows arches: amd64)", goos, goarch)
		}
	case "darwin":
		if goarch != "amd64" && goarch != "arm64" {
			return nil, fmt.Errorf("unsupported platform %s/%s (supported macOS arches: amd64, arm64)", goos, goarch)
		}
	default:
		return nil, fmt.Errorf("unsupported platform %s/%s (supported OSes: linux, windows, darwin)", goos, goarch)
	}

	return []bundleBinarySpec{
		{
			logicalName: assetArtifactMCP,
			archiveName: bundleArchiveBinaryName(assetArtifactMCP, goos, goarch),
			installName: executableNameForGOOS(assetArtifactMCP, goos),
		},
		{
			logicalName: assetArtifactWeb,
			archiveName: bundleArchiveBinaryName(assetArtifactWeb, goos, goarch),
			installName: executableNameForGOOS(assetArtifactWeb, goos),
		},
	}, nil
}

func (m *Manager) downloadRequiredAssets(ctx context.Context, stateDir string, release releaseResponse, requiredAssetNames []string, tempPrefix string) (string, map[string]string, error) {
	assets, err := mapRequiredAssets(release.Assets, requiredAssetNames)
	if err != nil {
		return "", nil, err
	}

	tmpDir, err := os.MkdirTemp(stateDir, tempPrefix)
	if err != nil {
		return "", nil, fmt.Errorf("create temp download dir: %w", err)
	}

	downloaded := map[string]string{}
	for _, name := range requiredAssetNames {
		asset := assets[name]
		dest := filepath.Join(tmpDir, name)
		if err := m.downloadFile(ctx, asset.BrowserDownloadURL, dest); err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", nil, fmt.Errorf("download release asset %q: %w", name, err)
		}
		downloaded[name] = dest
		if info, statErr := os.Stat(dest); statErr == nil {
			m.reportDetail("downloaded %s (%d bytes)", name, info.Size())
		}
	}

	m.reportStepOK("Downloaded release assets", release.TagName)
	return tmpDir, downloaded, nil
}

func (m *Manager) verifyAttestationsOrReport(ctx context.Context, downloaded map[string]string, isUpdate bool, scope Scope) error {
	if m.skipAttestationsCheck {
		m.reportStepOK("Verified attestations", "skipped (--skip-attestations-check)")
		m.reportDetail("attestation verification skipped by flag")
		return nil
	}

	if err := m.verifyDownloadedAssets(ctx, downloaded); err != nil {
		var attestationErr *attestationVerificationError
		if errors.As(err, &attestationErr) {
			m.reportStepFail("Verified attestations")
			m.reportMessageLine("Failed asset: %s", attestationErr.Asset)
			return formatAttestationVerificationFailure(attestationErr, commandForAttestationSkip(isUpdate, scope))
		}
		return err
	}

	m.reportStepOK("Verified attestations", "")
	return nil
}

func (m *Manager) extractDownloadedBundle(tmpDir string, downloaded map[string]string) (map[string]string, error) {
	bundleZipPath := downloaded[assetLocalArtifactZip]
	if bundleZipPath == "" {
		return nil, fmt.Errorf("missing downloaded release asset %q", assetLocalArtifactZip)
	}

	bundleDir := filepath.Join(tmpDir, "local-artifact")
	if err := os.MkdirAll(bundleDir, stateDirPerm); err != nil {
		return nil, fmt.Errorf("create local-artifact bundle extraction dir: %w", err)
	}

	specs, err := m.bundleBinarySpecs()
	if err != nil {
		return nil, err
	}

	archiveNames := make([]string, 0, len(specs))
	for _, spec := range specs {
		archiveNames = append(archiveNames, spec.archiveName)
	}
	bundleBinaries, err := extractBundleBinaries(bundleZipPath, bundleDir, archiveNames)
	legacyLayout := false
	if err != nil {
		legacyBinaries, legacyErr := extractBundleBinaries(bundleZipPath, bundleDir, []string{assetArtifactMCP, assetArtifactWeb})
		if legacyErr != nil {
			return nil, fmt.Errorf("extract %s: %w", assetLocalArtifactZip, err)
		}
		bundleBinaries = legacyBinaries
		legacyLayout = true
	}

	binariesByLogical := make(map[string]string, len(specs))
	for _, spec := range specs {
		name := spec.archiveName
		if legacyLayout {
			name = spec.logicalName
		}
		path := bundleBinaries[name]
		if path == "" {
			return nil, fmt.Errorf("bundle is missing expected binary %q", name)
		}
		binariesByLogical[spec.logicalName] = path
	}
	m.reportDetail("extracted bundle %s", assetLocalArtifactZip)
	return binariesByLogical, nil
}

func (m *Manager) installExtractedBinaries(ctx context.Context, bundleBinaries map[string]string, destinationDir string, mutations *mutationTracker, permissionHintDir string) ([]string, error) {
	specs, err := m.bundleBinarySpecs()
	if err != nil {
		return nil, err
	}
	binaryPaths := make([]string, 0, len(specs))

	installer := m.installBinary
	if installer == nil {
		installer = installBinary
	}

	for _, spec := range specs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		src := bundleBinaries[spec.logicalName]
		if src == "" {
			return nil, fmt.Errorf("missing extracted binary %q", spec.logicalName)
		}
		dst := filepath.Join(destinationDir, spec.installName)
		binaryPaths = append(binaryPaths, dst)
		if err := mutations.snapshotFile(dst); err != nil {
			return nil, err
		}
		if err := installer(src, dst); err != nil {
			if permissionHintDir != "" && errors.Is(err, os.ErrPermission) {
				return nil, fmt.Errorf("install %s into %s: %w (requires privileges to write %s)", spec.installName, destinationDir, err, permissionHintDir)
			}
			return nil, fmt.Errorf("install %s into %s: %w", spec.installName, destinationDir, err)
		}
		m.reportDetail("installed binary: %s", spec.installName)
	}

	return binaryPaths, nil
}
