package daemonctl

import "errors"

// ErrProcessRegistryMetadata marks non-fatal process registry data issues
// (for example malformed pid file names/payloads).
var ErrProcessRegistryMetadata = errors.New("process registry metadata issue")

// IsOnlyProcessRegistryMetadataIssues reports whether err is composed entirely
// of metadata-only registry issues.
func IsOnlyProcessRegistryMetadataIssues(err error) bool {
	if err == nil {
		return false
	}
	leafErrs := collectLeafErrors(err)
	if len(leafErrs) == 0 {
		return false
	}
	for _, leafErr := range leafErrs {
		if !errors.Is(leafErr, ErrProcessRegistryMetadata) {
			return false
		}
	}
	return true
}

func collectLeafErrors(err error) []error {
	if err == nil {
		return nil
	}
	var leafErrs []error
	appendLeafErrors(err, &leafErrs)
	return leafErrs
}

func appendLeafErrors(err error, out *[]error) {
	if err == nil {
		return
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		children := multi.Unwrap()
		if len(children) == 0 {
			*out = append(*out, err)
			return
		}
		for _, childErr := range children {
			appendLeafErrors(childErr, out)
		}
		return
	}
	if childErr := errors.Unwrap(err); childErr != nil {
		appendLeafErrors(childErr, out)
		return
	}
	*out = append(*out, err)
}
