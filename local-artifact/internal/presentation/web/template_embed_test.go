package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestIndexTemplateHasPrepaintThemeBootstrapAndSafeStorageGuards(t *testing.T) {
	rawTemplate, err := templateFiles.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read embedded template: %v", err)
	}

	templateText := string(rawTemplate)
	bootstrap := "applyTheme(loadTheme());"
	bootstrapIndex := strings.Index(templateText, bootstrap)
	if bootstrapIndex == -1 {
		t.Fatalf("expected prepaint theme bootstrap in template head")
	}

	styleIndex := strings.Index(templateText, "<style>")
	if styleIndex == -1 {
		t.Fatalf("expected style tag in template")
	}
	if bootstrapIndex > styleIndex {
		t.Fatalf("expected prepaint bootstrap before style application")
	}

	setGuardPattern := regexp.MustCompile(`function\s+saveStoredTheme\([^)]*\)[\s\S]*?try[\s\S]*?window\.localStorage\.setItem\(`)
	if !setGuardPattern.MatchString(templateText) {
		t.Fatalf("expected guarded localStorage set wrapper")
	}

	getGuardPattern := regexp.MustCompile(`function\s+loadStoredTheme\([^)]*\)[\s\S]*?try[\s\S]*?window\.localStorage\.getItem\(`)
	if !getGuardPattern.MatchString(templateText) {
		t.Fatalf("expected guarded localStorage get wrapper")
	}

	if !strings.Contains(templateText, "catch (error)") {
		t.Fatalf("expected try/catch guards for storage access")
	}

	if count := strings.Count(templateText, `name="csrf_token"`); count < 2 {
		t.Fatalf("expected csrf token hidden fields in insert and delete forms, got %d", count)
	}

	if !strings.Contains(templateText, `refInput.name = 'ref'`) {
		t.Fatalf("expected bulk delete selection to submit refs")
	}

	if !strings.Contains(templateText, `response.headers.get('X-Artifact-MimeType')`) {
		t.Fatalf("expected viewer to prefer artifact mime metadata header for previews")
	}
}
