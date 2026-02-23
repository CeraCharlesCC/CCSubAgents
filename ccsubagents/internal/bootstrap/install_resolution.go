package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) resolveReleaseForInstall(ctx context.Context) (releaseResponse, error) {
	if err := ctx.Err(); err != nil {
		return releaseResponse{}, err
	}

	home, cwd, settings, err := m.resolveInstallSettingsContext()
	if err != nil {
		return releaseResponse{}, err
	}

	requestedTag := normalizeVersionTag(m.installVersionRaw)
	pinnedTag := normalizeVersionTag(settings.PinnedVersion)

	effectiveTag := requestedTag
	if pinnedTag != "" {
		if requestedTag == "" {
			return releaseResponse{}, fmt.Errorf("install is pinned to %s; rerun with --version %s or edit settings.json to change/remove pinned-version", pinnedTag, pinnedTag)
		}
		if requestedTag != pinnedTag {
			return releaseResponse{}, fmt.Errorf("install is pinned to %s; requested --version %s does not match", pinnedTag, requestedTag)
		}
		effectiveTag = pinnedTag
	}
	if m.pinRequested && effectiveTag == "" {
		return releaseResponse{}, ErrPinnedRequiresVersion
	}

	release, err := m.fetchReleaseForVersion(ctx, effectiveTag)
	if err != nil {
		return releaseResponse{}, err
	}

	if !m.pinRequested {
		return release, nil
	}

	pinPath, pinScope, err := choosePinWritePath(cwd, home)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("determine pin settings path: %w", err)
	}
	if err := writePinnedVersion(pinPath, effectiveTag); err != nil {
		return releaseResponse{}, err
	}
	m.reportDetail("saved pinned-version %s to %s settings (%s)", effectiveTag, pinScope, pinPath)

	return release, nil
}

func (m *Manager) resolveReleaseForUpdate(ctx context.Context) (releaseResponse, error) {
	if err := ctx.Err(); err != nil {
		return releaseResponse{}, err
	}

	_, _, settings, err := m.resolveInstallSettingsContext()
	if err != nil {
		return releaseResponse{}, err
	}

	pinnedTag := normalizeVersionTag(settings.PinnedVersion)
	if pinnedTag != "" {
		return releaseResponse{}, fmt.Errorf("update is blocked because pinned-version is set to %s; edit settings.json to clear pinned-version before updating", pinnedTag)
	}

	return m.fetchReleaseForVersion(ctx, "")
}

func (m *Manager) fetchReleaseForVersion(ctx context.Context, tag string) (releaseResponse, error) {
	requestedTag := normalizeVersionTag(tag)
	if requestedTag == "" {
		return m.fetchLatestRelease(ctx)
	}

	release, err := m.fetchReleaseByTag(ctx, requestedTag)
	if err == nil {
		return release, nil
	}

	if errors.Is(err, errReleaseNotFound) {
		m.reportWarning(
			"Requested version does not exist",
			fmt.Sprintf("Version %s was not found in %s.", requestedTag, releaseRepo),
		)
		return releaseResponse{}, fmt.Errorf("requested version %s was not found", requestedTag)
	}

	return releaseResponse{}, err
}

func (m *Manager) resolveInstallSettingsContext() (home, cwd string, settings installSettings, err error) {
	homeDir := m.homeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err = homeDir()
	if err != nil {
		return "", "", installSettings{}, fmt.Errorf("determine home directory: %w", err)
	}

	workingDir := m.workingDir
	if workingDir == nil {
		workingDir = os.Getwd
	}
	cwd, err = workingDir()
	if err != nil {
		return "", "", installSettings{}, fmt.Errorf("determine current working directory: %w", err)
	}
	cwd = filepath.Clean(cwd)

	settings, err = loadMergedInstallSettings(home, cwd)
	if err != nil {
		return "", "", installSettings{}, fmt.Errorf("load install settings: %w", err)
	}

	return home, cwd, settings, nil
}
