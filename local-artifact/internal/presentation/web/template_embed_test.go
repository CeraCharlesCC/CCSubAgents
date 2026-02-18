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
	bootstrapPattern := regexp.MustCompile(`applyTheme\s*\(\s*loadTheme\s*\(\s*\)\s*\)\s*;?`)
	bootstrapLoc := bootstrapPattern.FindStringIndex(templateText)
	if bootstrapLoc == nil {
		t.Fatalf("expected prepaint theme bootstrap in template head")
	}
	bootstrapIndex := bootstrapLoc[0]

	stylePattern := regexp.MustCompile(`<style\b`)
	styleLoc := stylePattern.FindStringIndex(templateText)
	if styleLoc == nil {
		t.Fatalf("expected style tag in template")
	}
	styleIndex := styleLoc[0]
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

	if !strings.Contains(templateText, `aria-pressed="false"`) {
		t.Fatalf("expected theme toggle to initialize aria-pressed state")
	}
	if !strings.Contains(templateText, `themeToggle.setAttribute('aria-pressed'`) {
		t.Fatalf("expected theme toggle script to update aria-pressed state")
	}
}
