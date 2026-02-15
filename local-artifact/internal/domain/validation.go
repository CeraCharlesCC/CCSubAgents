package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNameLength = 200

var refPattern = regexp.MustCompile(`^\d{8}T\d{6}(?:\.\d{3})?Z-[0-9a-f]{16}$`)

func normalizeAndValidateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidName)
	}
	if len(name) > maxNameLength {
		return "", fmt.Errorf("%w: max length is %d", ErrInvalidName, maxNameLength)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: contains control characters", ErrInvalidName)
		}
	}
	return name, nil
}

func normalizeAndValidateRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ErrRefRequired
	}
	if !refPattern.MatchString(ref) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return ref, nil
}

func normalizeSelector(sel Selector) (Selector, error) {
	hasRef := strings.TrimSpace(sel.Ref) != ""
	hasName := strings.TrimSpace(sel.Name) != ""
	if !hasRef && !hasName {
		return Selector{}, ErrRefOrName
	}
	if hasRef && hasName {
		return Selector{}, ErrRefAndNameMutuallyExclusive
	}
	if hasRef {
		ref, err := normalizeAndValidateRef(sel.Ref)
		if err != nil {
			return Selector{}, err
		}
		return Selector{Ref: ref}, nil
	}
	name, err := normalizeAndValidateName(sel.Name)
	if err != nil {
		return Selector{}, err
	}
	return Selector{Name: name}, nil
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}
	if !utf8.ValidString(prefix) {
		return "", fmt.Errorf("%w: prefix must be valid UTF-8", ErrInvalidInput)
	}
	for _, r := range prefix {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: prefix contains control characters", ErrInvalidInput)
		}
	}
	if len(prefix) > maxNameLength {
		return "", fmt.Errorf("%w: prefix max length is %d", ErrInvalidInput, maxNameLength)
	}
	return prefix, nil
}
