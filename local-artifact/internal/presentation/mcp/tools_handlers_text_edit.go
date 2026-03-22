package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	daemon "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemonapi"
)

type textEditArgs struct {
	Operation string          `json:"operation"`
	Artifact  daemon.Selector `json:"artifact"`
	Text      string          `json:"text,omitempty"`
	Patch     string          `json:"patch,omitempty"`
}

type unifiedTextPatch struct {
	Hunks []unifiedTextHunk
}

type unifiedTextHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []unifiedTextHunkLine
}

type unifiedTextHunkLine struct {
	Kind      byte
	Text      string
	NoNewline bool
}

var unifiedTextHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?: .*)?$`)

const textEditInvalidArgumentsMessage = "Invalid arguments: expected {operation, artifact, text?|patch?}"

func (s *Server) toolEditText(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args textEditArgs
	decoder := json.NewDecoder(bytes.NewReader(argsRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return toolError(textEditInvalidArgumentsMessage), nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return toolError(textEditInvalidArgumentsMessage), nil
	}

	operation := strings.TrimSpace(args.Operation)
	selector := daemon.Selector{Ref: strings.TrimSpace(args.Artifact.Ref), Name: strings.TrimSpace(args.Artifact.Name)}
	if err := artifacts.ValidateSelector(artifacts.Selector{Ref: selector.Ref, Name: selector.Name}); err != nil {
		return toolErrorFromErr(err), nil
	}

	switch operation {
	case "append":
		if args.Text == "" || args.Patch != "" {
			return toolError(textEditInvalidArgumentsMessage), nil
		}
	case "patch":
		if args.Patch == "" || args.Text != "" {
			return toolError(textEditInvalidArgumentsMessage), nil
		}
	default:
		return toolErrorFromErr(fmt.Errorf("%w: operation must be append or patch", artifacts.ErrInvalidInput)), nil
	}

	workspace := s.currentWorkspace(ctx)
	got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: workspace, Selector: selector})
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	a := got.Artifact
	if strings.TrimSpace(a.Name) == "" {
		return toolErrorFromErr(fmt.Errorf("%w: artifact referenced by ref has no name", artifacts.ErrInvalidInput)), nil
	}
	if !artifactIsText(a) {
		return toolErrorFromErr(fmt.Errorf("%w: artifact is not editable as text", artifacts.ErrInvalidInput)), nil
	}

	data, err := base64.StdEncoding.DecodeString(got.DataBase64)
	if err != nil {
		return toolError("internal error: invalid daemon payload"), nil
	}
	if !utf8.Valid(data) {
		return toolErrorFromErr(fmt.Errorf("%w: artifact payload is not valid UTF-8", artifacts.ErrInvalidInput)), nil
	}

	currentText := string(data)
	updatedText := currentText
	statusText := "appended"
	if operation == "append" {
		updatedText = currentText + args.Text
	} else {
		updatedText, err = applyUnifiedTextPatch(currentText, args.Patch)
		if err != nil {
			return toolErrorFromErr(err), nil
		}
		statusText = "patched"
	}
	if updatedText == currentText {
		return toolErrorFromErr(fmt.Errorf("%w: edit produced no changes", artifacts.ErrInvalidInput)), nil
	}

	saved, err := s.daemon().SaveArtifact(ctx, daemon.SaveArtifactRequest{
		Workspace:       workspace,
		Name:            a.Name,
		DataBase64:      base64.StdEncoding.EncodeToString([]byte(updatedText)),
		Kind:            a.Kind,
		MimeType:        a.MimeType,
		Filename:        a.Filename,
		ExpectedPrevRef: a.Ref,
	})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	nameEsc := url.PathEscape(saved.Name)
	out := toSaveOut(saved, nameEsc)
	return toolResult{
		Content: []any{
			textContent(statusText),
		},
		StructuredContent: out,
	}, nil
}

func applyUnifiedTextPatch(currentText, patch string) (string, error) {
	parsed, err := parseUnifiedTextPatch(patch)
	if err != nil {
		return "", err
	}

	originalLines, originalTrailingNewline := splitTextLines(currentText)
	resultLines := make([]string, 0, len(originalLines))
	originalIndex := 0
	touchesEOF := false
	var expectedOldTrailingNewline *bool
	var resultTrailingNewline *bool

	for hunkIndex, hunk := range parsed.Hunks {
		oldStartIndex := 0
		if hunk.OldStart > 0 {
			oldStartIndex = hunk.OldStart - 1
		}
		if oldStartIndex < originalIndex || oldStartIndex > len(originalLines) {
			return "", fmt.Errorf("%w: patch hunk position is out of range", artifacts.ErrInvalidInput)
		}

		resultLines = append(resultLines, originalLines[originalIndex:oldStartIndex]...)
		matchIndex := oldStartIndex
		oldConsumed := 0
		newProduced := 0

		for _, line := range hunk.Lines {
			switch line.Kind {
			case ' ':
				if matchIndex >= len(originalLines) || originalLines[matchIndex] != line.Text {
					return "", fmt.Errorf("%w: patch hunk does not apply", artifacts.ErrInvalidInput)
				}
				resultLines = append(resultLines, originalLines[matchIndex])
				matchIndex++
				oldConsumed++
				newProduced++
			case '-':
				if matchIndex >= len(originalLines) || originalLines[matchIndex] != line.Text {
					return "", fmt.Errorf("%w: patch hunk does not apply", artifacts.ErrInvalidInput)
				}
				matchIndex++
				oldConsumed++
			case '+':
				resultLines = append(resultLines, line.Text)
				newProduced++
			default:
				return "", fmt.Errorf("%w: unsupported patch line prefix", artifacts.ErrInvalidInput)
			}

			if hunkIndex == len(parsed.Hunks)-1 && line.NoNewline {
				falseValue := false
				switch line.Kind {
				case ' ':
					expectedOldTrailingNewline = &falseValue
					resultTrailingNewline = &falseValue
				case '-':
					expectedOldTrailingNewline = &falseValue
				case '+':
					resultTrailingNewline = &falseValue
				}
			}
		}

		if oldConsumed != hunk.OldCount || newProduced != hunk.NewCount {
			return "", fmt.Errorf("%w: malformed patch hunk counts", artifacts.ErrInvalidInput)
		}

		originalIndex = matchIndex
		if hunkIndex == len(parsed.Hunks)-1 {
			touchesEOF = oldStartIndex+hunk.OldCount == len(originalLines)
			if !touchesEOF && (expectedOldTrailingNewline != nil || resultTrailingNewline != nil) {
				return "", fmt.Errorf("%w: no-newline marker is only supported when the final hunk reaches EOF", artifacts.ErrInvalidInput)
			}
		}
	}

	resultLines = append(resultLines, originalLines[originalIndex:]...)
	if touchesEOF {
		switch {
		case expectedOldTrailingNewline != nil:
			if originalTrailingNewline != *expectedOldTrailingNewline {
				return "", fmt.Errorf("%w: patch EOF newline marker does not match the current artifact content", artifacts.ErrInvalidInput)
			}
		case len(originalLines) > 0 && !originalTrailingNewline:
			return "", fmt.Errorf("%w: patch expected the current artifact to end with a newline", artifacts.ErrInvalidInput)
		}
	}

	finalTrailingNewline := originalTrailingNewline
	if touchesEOF {
		switch {
		case resultTrailingNewline != nil:
			finalTrailingNewline = *resultTrailingNewline
		case len(resultLines) == 0:
			finalTrailingNewline = false
		default:
			finalTrailingNewline = true
		}
	}

	return joinTextLines(resultLines, finalTrailingNewline), nil
}

func parseUnifiedTextPatch(patch string) (unifiedTextPatch, error) {
	trimmed := strings.TrimSpace(patch)
	if trimmed == "" {
		return unifiedTextPatch{}, fmt.Errorf("%w: patch is required", artifacts.ErrInvalidInput)
	}

	rawLines := strings.Split(patch, "\n")
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	parsed := unifiedTextPatch{}
	seenHunk := false
	diffHeaderCount := 0

	for i := 0; i < len(rawLines); {
		line := rawLines[i]
		if strings.HasPrefix(line, "diff --git ") {
			diffHeaderCount++
			if diffHeaderCount > 1 {
				return unifiedTextPatch{}, fmt.Errorf("%w: multi-file patches are not supported", artifacts.ErrInvalidInput)
			}
			if seenHunk {
				return unifiedTextPatch{}, fmt.Errorf("%w: multi-file patches are not supported", artifacts.ErrInvalidInput)
			}
			i++
			continue
		}

		if strings.TrimSpace(line) == "" && !seenHunk {
			i++
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			seenHunk = true
			hunk, nextIndex, err := parseUnifiedTextHunk(rawLines, i)
			if err != nil {
				return unifiedTextPatch{}, err
			}
			parsed.Hunks = append(parsed.Hunks, hunk)
			i = nextIndex
			continue
		}

		if seenHunk {
			if strings.TrimSpace(line) == "" && onlyBlankLinesRemain(rawLines[i:]) {
				break
			}
			return unifiedTextPatch{}, fmt.Errorf("%w: multi-file patches are not supported", artifacts.ErrInvalidInput)
		}

		if err := validateUnifiedPatchHeaderLine(line); err != nil {
			return unifiedTextPatch{}, err
		}
		i++
	}

	if len(parsed.Hunks) == 0 {
		return unifiedTextPatch{}, fmt.Errorf("%w: patch must contain at least one unified diff hunk", artifacts.ErrInvalidInput)
	}
	for hunkIndex := 0; hunkIndex < len(parsed.Hunks)-1; hunkIndex++ {
		for _, line := range parsed.Hunks[hunkIndex].Lines {
			if line.NoNewline {
				return unifiedTextPatch{}, fmt.Errorf("%w: no-newline markers are only supported in the final hunk", artifacts.ErrInvalidInput)
			}
		}
	}

	return parsed, nil
}

func parseUnifiedTextHunk(lines []string, start int) (unifiedTextHunk, int, error) {
	match := unifiedTextHunkHeader.FindStringSubmatch(lines[start])
	if match == nil {
		return unifiedTextHunk{}, start, fmt.Errorf("%w: invalid unified diff hunk header", artifacts.ErrInvalidInput)
	}

	oldStart, _ := strconv.Atoi(match[1])
	oldCount := 1
	if match[2] != "" {
		oldCount, _ = strconv.Atoi(match[2])
	}
	newStart, _ := strconv.Atoi(match[3])
	newCount := 1
	if match[4] != "" {
		newCount, _ = strconv.Atoi(match[4])
	}
	if (oldStart == 0 && oldCount > 0) || (newStart == 0 && newCount > 0) {
		return unifiedTextHunk{}, start, fmt.Errorf("%w: invalid unified diff hunk range", artifacts.ErrInvalidInput)
	}

	hunk := unifiedTextHunk{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount}
	oldSeen := 0
	newSeen := 0

	i := start + 1
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "@@ ") {
			break
		}
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			break
		}
		if line == `\ No newline at end of file` {
			if len(hunk.Lines) == 0 {
				return unifiedTextHunk{}, start, fmt.Errorf("%w: malformed no-newline marker", artifacts.ErrInvalidInput)
			}
			hunk.Lines[len(hunk.Lines)-1].NoNewline = true
			i++
			continue
		}
		if len(line) == 0 {
			return unifiedTextHunk{}, start, fmt.Errorf("%w: malformed unified diff hunk line", artifacts.ErrInvalidInput)
		}

		prefix := line[0]
		text := line[1:]
		switch prefix {
		case ' ':
			oldSeen++
			newSeen++
		case '-':
			oldSeen++
		case '+':
			newSeen++
		default:
			return unifiedTextHunk{}, start, fmt.Errorf("%w: malformed unified diff hunk line", artifacts.ErrInvalidInput)
		}
		hunk.Lines = append(hunk.Lines, unifiedTextHunkLine{Kind: prefix, Text: text})
		i++
	}

	if oldSeen != hunk.OldCount || newSeen != hunk.NewCount {
		return unifiedTextHunk{}, start, fmt.Errorf("%w: malformed unified diff hunk counts", artifacts.ErrInvalidInput)
	}

	return hunk, i, nil
}

func validateUnifiedPatchHeaderLine(line string) error {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	for _, prefix := range []string{"new file mode ", "deleted file mode ", "rename from ", "rename to ", "copy from ", "copy to ", "Binary files ", "GIT binary patch"} {
		if strings.HasPrefix(trimmed, prefix) {
			return fmt.Errorf("%w: unsupported patch header %q", artifacts.ErrInvalidInput, trimmed)
		}
	}
	if strings.HasPrefix(trimmed, "--- ") || strings.HasPrefix(trimmed, "+++ ") {
		if strings.Contains(trimmed, "/dev/null") {
			return fmt.Errorf("%w: create/delete patches are not supported", artifacts.ErrInvalidInput)
		}
		return nil
	}
	for _, prefix := range []string{"index ", "diff --git ", "old mode ", "new mode ", "similarity index ", "dissimilarity index ", "Index: "} {
		if strings.HasPrefix(trimmed, prefix) {
			return nil
		}
	}
	return fmt.Errorf("%w: unsupported patch header %q", artifacts.ErrInvalidInput, trimmed)
}

func splitTextLines(text string) ([]string, bool) {
	if text == "" {
		return nil, false
	}
	trailingNewline := strings.HasSuffix(text, "\n")
	parts := strings.Split(text, "\n")
	if trailingNewline {
		parts = parts[:len(parts)-1]
	}
	return parts, trailingNewline
}

func joinTextLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}
	joined := strings.Join(lines, "\n")
	if trailingNewline {
		joined += "\n"
	}
	return joined
}

func onlyBlankLinesRemain(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}
