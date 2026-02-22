package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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

	bundleBinaries, err := extractBundleBinaries(bundleZipPath, bundleDir, []string{assetArtifactMCP, assetArtifactWeb})
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", assetLocalArtifactZip, err)
	}
	m.reportDetail("extracted bundle %s", assetLocalArtifactZip)
	return bundleBinaries, nil
}

func (m *Manager) installExtractedBinaries(ctx context.Context, bundleBinaries map[string]string, destinationDir string, mutations *mutationTracker, permissionHintDir string) ([]string, error) {
	binaryPaths := []string{
		filepath.Join(destinationDir, assetArtifactMCP),
		filepath.Join(destinationDir, assetArtifactWeb),
	}

	installer := m.installBinary
	if installer == nil {
		installer = installBinary
	}

	for _, binaryName := range []string{assetArtifactMCP, assetArtifactWeb} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		src := bundleBinaries[binaryName]
		dst := filepath.Join(destinationDir, binaryName)
		if err := mutations.snapshotFile(dst); err != nil {
			return nil, err
		}
		if err := installer(src, dst); err != nil {
			if permissionHintDir != "" && errors.Is(err, os.ErrPermission) {
				return nil, fmt.Errorf("install %s into %s: %w (requires privileges to write %s)", binaryName, destinationDir, err, permissionHintDir)
			}
			return nil, fmt.Errorf("install %s into %s: %w", binaryName, destinationDir, err)
		}
		m.reportDetail("installed binary: %s", binaryName)
	}

	return binaryPaths, nil
}
