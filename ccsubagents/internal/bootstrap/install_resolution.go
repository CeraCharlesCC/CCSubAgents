package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pendingPinWrite struct {
	path       string
	scope      settingsScope
	versionTag string
}

func (m *Manager) resolveReleaseForInstall(ctx context.Context) (releaseResponse, error) {
	if err := ctx.Err(); err != nil {
		return releaseResponse{}, err
	}
	m.pendingPinWrite = nil

	home, settingsRoot, settings, err := m.resolveInstallSettingsContext()
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

	pinPath, pinScope, err := choosePinWritePath(settingsRoot, home)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("determine pin settings path: %w", err)
	}
	m.pendingPinWrite = &pendingPinWrite{
		path:       pinPath,
		scope:      pinScope,
		versionTag: effectiveTag,
	}

	return release, nil
}

func (m *Manager) resolveReleaseForUpdate(ctx context.Context) (releaseResponse, error) {
	if err := ctx.Err(); err != nil {
		return releaseResponse{}, err
	}
	m.pendingPinWrite = nil

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

func (m *Manager) resolveInstallSettingsContext() (home, settingsRoot string, settings installSettings, err error) {
	homeDir := m.homeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err = homeDir()
	if err != nil {
		return "", "", installSettings{}, fmt.Errorf("determine home directory: %w", err)
	}

	settingsRoot = strings.TrimSpace(m.installSettingsRoot)
	if settingsRoot == "" {
		workingDir := m.workingDir
		if workingDir == nil {
			workingDir = os.Getwd
		}
		settingsRoot, err = workingDir()
		if err != nil {
			return "", "", installSettings{}, fmt.Errorf("determine current working directory: %w", err)
		}
	}
	settingsRoot = filepath.Clean(settingsRoot)

	settings, err = loadMergedInstallSettings(home, settingsRoot)
	if err != nil {
		return "", "", installSettings{}, fmt.Errorf("load install settings: %w", err)
	}

	return home, settingsRoot, settings, nil
}

func (m *Manager) persistPendingPinWrite(mutations *mutationTracker) error {
	if m.pendingPinWrite == nil {
		return nil
	}

	pending := *m.pendingPinWrite
	if mutations != nil {
		if err := mutations.ensureParentDir(pending.path); err != nil {
			return err
		}
		if err := mutations.snapshotFile(pending.path); err != nil {
			return err
		}
	}

	if err := writePinnedVersion(pending.path, pending.versionTag); err != nil {
		return err
	}
	m.reportDetail("saved pinned-version %s to %s settings (%s)", pending.versionTag, pending.scope, pending.path)
	m.pendingPinWrite = nil
	return nil
}
