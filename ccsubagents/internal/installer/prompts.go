package installer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/state"
)

func parseCommaSeparatedChoices(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func makeCustomConfigTarget(base string) installConfigTarget {
	cleanBase := filepath.Clean(base)
	return installConfigTarget{
		settingsPath: filepath.Join(cleanBase, "data", "Machine", "settings.json"),
		mcpPath:      filepath.Join(cleanBase, "data", "User", "mcp.json"),
	}
}

func trimConfigFileSuffix(path string, suffixParts ...string) (string, bool) {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	suffix := filepath.Clean(filepath.Join(suffixParts...))
	if !strings.HasSuffix(cleanPath, suffix) {
		return "", false
	}

	base := strings.TrimSuffix(cleanPath, suffix)
	base = strings.TrimRight(base, string(os.PathSeparator))
	if strings.TrimSpace(base) == "" {
		base = string(os.PathSeparator)
	}
	if volume := filepath.VolumeName(base); volume != "" && base == volume {
		base += string(os.PathSeparator)
	}

	return filepath.Clean(base), true
}

func describeGlobalInstallTargetPath(home string, target installConfigTarget) string {
	settingsPath := filepath.Clean(strings.TrimSpace(target.settingsPath))
	mcpPath := filepath.Clean(strings.TrimSpace(target.mcpPath))

	settingsRoot, settingsHasRoot := trimConfigFileSuffix(settingsPath, "data", "Machine", "settings.json")
	if !settingsHasRoot {
		settingsRoot, settingsHasRoot = trimConfigFileSuffix(settingsPath, "User", "settings.json")
	}
	mcpRoot, mcpHasRoot := trimConfigFileSuffix(mcpPath, "data", "User", "mcp.json")
	if !mcpHasRoot {
		mcpRoot, mcpHasRoot = trimConfigFileSuffix(mcpPath, "User", "mcp.json")
	}
	if settingsHasRoot && mcpHasRoot && filepath.Clean(settingsRoot) == filepath.Clean(mcpRoot) {
		return toHomeTildePath(home, settingsRoot)
	}

	settingsDisplay := toHomeTildePath(home, settingsPath)
	mcpDisplay := toHomeTildePath(home, mcpPath)
	if settingsDisplay == mcpDisplay {
		return settingsDisplay
	}

	return fmt.Sprintf("settings: %s; mcp: %s", settingsDisplay, mcpDisplay)
}

func (r *Runner) promptGlobalInstallTargets(ctx context.Context, home string, paths installPaths) ([]installConfigTarget, error) {
	input := r.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := r.promptOut
	if output == nil {
		output = os.Stdout
	}

	reader := bufio.NewReader(input)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		insidersDisplay := describeGlobalInstallTargetPath(home, installConfigTarget{
			settingsPath: paths.insiders.settingsPath,
			mcpPath:      paths.insiders.mcpPath,
		})
		stableDisplay := describeGlobalInstallTargetPath(home, installConfigTarget{
			settingsPath: paths.stable.settingsPath,
			mcpPath:      paths.stable.mcpPath,
		})
		desktopInsidersDisplay := describeGlobalInstallTargetPath(home, installConfigTarget{
			settingsPath: paths.desktopInsiders.settingsPath,
			mcpPath:      paths.desktopInsiders.mcpPath,
		})
		desktopStableDisplay := describeGlobalInstallTargetPath(home, installConfigTarget{
			settingsPath: paths.desktopStable.settingsPath,
			mcpPath:      paths.desktopStable.mcpPath,
		})

		var prompt bytes.Buffer
		prompt.WriteString("Where should ccsubagents be installed?\n\n")
		fmt.Fprintf(&prompt, "[1] VS Code Server — Insiders   (%s)\n", insidersDisplay)
		fmt.Fprintf(&prompt, "[2] VS Code Server — Stable     (%s)\n", stableDisplay)
		fmt.Fprintf(&prompt, "[3] VS Code Desktop — Insiders  (%s)\n", desktopInsidersDisplay)
		fmt.Fprintf(&prompt, "[4] VS Code Desktop — Stable    (%s)\n", desktopStableDisplay)
		prompt.WriteString("[5] Custom path(s)\n")
		prompt.WriteString("\n")
		prompt.WriteString("Choice (comma-separated, e.g. 1,2): ")

		if _, err := io.Copy(output, &prompt); err != nil {
			return nil, fmt.Errorf("write install target prompt: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read install target selection: %w", err)
		}

		choices := parseCommaSeparatedChoices(line)
		selected := map[string]struct{}{}
		valid := true
		for _, choice := range choices {
			switch choice {
			case "1", "2", "3", "4", "5":
				selected[choice] = struct{}{}
			default:
				valid = false
			}
		}

		if len(selected) == 0 || !valid {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("global install target selection canceled")
			}
			if _, writeErr := io.WriteString(output, "Invalid selection. Enter comma-separated values using 1 through 5.\n\n"); writeErr != nil {
				return nil, fmt.Errorf("write install target prompt: %w", writeErr)
			}
			continue
		}

		targets := make([]installConfigTarget, 0, len(selected)+1)
		if _, ok := selected["1"]; ok {
			targets = append(targets, installConfigTarget{settingsPath: paths.insiders.settingsPath, mcpPath: paths.insiders.mcpPath})
		}
		if _, ok := selected["2"]; ok {
			targets = append(targets, installConfigTarget{settingsPath: paths.stable.settingsPath, mcpPath: paths.stable.mcpPath})
		}
		if _, ok := selected["3"]; ok {
			targets = append(targets, installConfigTarget{settingsPath: paths.desktopInsiders.settingsPath, mcpPath: paths.desktopInsiders.mcpPath})
		}
		if _, ok := selected["4"]; ok {
			targets = append(targets, installConfigTarget{settingsPath: paths.desktopStable.settingsPath, mcpPath: paths.desktopStable.mcpPath})
		}
		if _, ok := selected["5"]; ok {
			if _, err := io.WriteString(output, "Enter custom target path(s), comma-separated: "); err != nil {
				return nil, fmt.Errorf("write custom target prompt: %w", err)
			}
			customLine, customErr := reader.ReadString('\n')
			if customErr != nil && !errors.Is(customErr, io.EOF) {
				return nil, fmt.Errorf("read custom target paths: %w", customErr)
			}

			customPaths := parseCommaSeparatedChoices(customLine)
			if len(customPaths) == 0 {
				if errors.Is(customErr, io.EOF) {
					return nil, errors.New("custom install target selection canceled")
				}
				if _, writeErr := io.WriteString(output, "Custom paths cannot be empty.\n\n"); writeErr != nil {
					return nil, fmt.Errorf("write custom target prompt: %w", writeErr)
				}
				continue
			}

			for _, customPath := range customPaths {
				resolved := resolveConfiguredPath(home, customPath)
				if strings.TrimSpace(resolved) == "" {
					continue
				}
				targets = append(targets, makeCustomConfigTarget(resolved))
			}
		}

		targets = uniqueInstallTargets(targets)
		if len(targets) == 0 {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("global install target selection canceled")
			}
			if _, writeErr := io.WriteString(output, "No valid targets selected.\n\n"); writeErr != nil {
				return nil, fmt.Errorf("write install target prompt: %w", writeErr)
			}
			continue
		}

		return targets, nil
	}
}

func (r *Runner) promptLocalInstallMode(ctx context.Context) (state.LocalInstallMode, error) {
	input := r.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := r.promptOut
	if output == nil {
		output = os.Stdout
	}
	reader := bufio.NewReader(input)

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := io.WriteString(output, "Choose local install usage mode:\n"); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  1. Personal use\n"); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}
		if _, err := io.WriteString(output, "  2. Team / project-wide use\n"); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}
		if _, err := io.WriteString(output, "Enter choice [1-2]: "); err != nil {
			return "", fmt.Errorf("write local mode prompt: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read local mode selection: %w", err)
		}
		switch strings.TrimSpace(line) {
		case "1":
			return state.LocalInstallModePersonal, nil
		case "2":
			return state.LocalInstallModeTeam, nil
		default:
			if errors.Is(err, io.EOF) {
				return "", errors.New("local mode selection canceled")
			}
			if _, writeErr := io.WriteString(output, "Invalid selection. Enter 1 or 2.\n\n"); writeErr != nil {
				return "", fmt.Errorf("write local mode prompt: %w", writeErr)
			}
		}
	}
}

func (r *Runner) confirmTeamLocalUninstall(ctx context.Context, installRoot string) error {
	input := r.promptIn
	if input == nil {
		input = os.Stdin
	}
	output := r.promptOut
	if output == nil {
		output = os.Stdout
	}
	reader := bufio.NewReader(input)

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Team local install at %s. Type YES to confirm uninstall: ", installRoot); err != nil {
		return fmt.Errorf("write team uninstall confirmation prompt: %w", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read team uninstall confirmation: %w", err)
	}
	if strings.TrimSpace(line) != "YES" {
		return errors.New("local uninstall canceled")
	}
	return nil
}
