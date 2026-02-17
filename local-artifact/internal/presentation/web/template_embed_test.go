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
	bootstrap := "document.documentElement.dataset.theme = loadStoredTheme() || systemTheme();"
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

	setGuardPattern := regexp.MustCompile(`(?s)function saveStoredTheme\(theme\)\s*\{\s*try\s*\{[^}]*window\.localStorage\.setItem\(storageKey,\s*theme\)`)
	if !setGuardPattern.MatchString(templateText) {
		t.Fatalf("expected guarded localStorage set wrapper")
	}

	getGuardPattern := regexp.MustCompile(`(?s)function loadStoredTheme\(\)\s*\{\s*try\s*\{[^}]*window\.localStorage\.getItem\(storageKey\)`)
	if !getGuardPattern.MatchString(templateText) {
		t.Fatalf("expected guarded localStorage get wrapper")
	}

	if !strings.Contains(templateText, "catch (error)") {
		t.Fatalf("expected try/catch guards for storage access")
	}
}
