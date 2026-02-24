package installer

import (
	"fmt"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/release"
)

func (r *Runner) statusf(format string, args ...any) {
	if r.statusOut == nil {
		return
	}
	_, _ = fmt.Fprintf(r.statusOut, format, args...)
}

func (r *Runner) reportVersionHeader(tag string) {
	r.statusf("ccsubagents %s\n", tag)
}

func (r *Runner) reportStepOK(summary, trailing string) {
	if strings.TrimSpace(trailing) == "" {
		r.statusf("✓ %s\n", summary)
		return
	}
	r.statusf("✓ %s (%s)\n", summary, trailing)
}

func (r *Runner) reportStepFail(summary string) {
	r.statusf("✗ %s\n", summary)
}

func (r *Runner) reportDetail(format string, args ...any) {
	if !r.verbose {
		return
	}
	r.statusf("  %s\n", fmt.Sprintf(format, args...))
}

func (r *Runner) reportMessageLine(format string, args ...any) {
	r.statusf("  %s\n", fmt.Sprintf(format, args...))
}

func (r *Runner) reportWarning(headline string, details ...string) {
	r.statusf("\n⚠ %s\n", headline)
	for _, detail := range details {
		if strings.TrimSpace(detail) == "" {
			continue
		}
		r.statusf("  %s\n", detail)
	}
}

func (r *Runner) reportCompletion(command string) {
	r.statusf("%s complete.\n", command)
}

func commandNameForInstallOrUpdate(isUpdate bool) string {
	if isUpdate {
		return "Update"
	}
	return "Install"
}

func commandForAttestationSkip(isUpdate bool, scope Scope) string {
	command := "install"
	if isUpdate {
		command = "update"
	}
	args := []string{"ccsubagents", command}
	if scope == ScopeLocal {
		args = append(args, "--scope=local")
	}
	args = append(args, "--skip-attestations-check")
	return strings.Join(args, " ")
}

func formatAttestationVerificationFailure(attestationErr *release.AttestationVerificationError, skipCommand string) error {
	if attestationErr == nil {
		return fmt.Errorf("Error: attestation verification failed\nTo skip verification: %s\n(not recommended for production use)", skipCommand)
	}
	return fmt.Errorf("Error: %w\nTo skip verification: %s\n(not recommended for production use)", attestationErr, skipCommand)
}
