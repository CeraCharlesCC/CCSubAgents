package main

import "testing"

func TestLooksLikeRef_StrictPattern(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid ref", value: "20260227T120000Z-aaaaaaaaaaaaaaaa", want: true},
		{name: "valid ref with millis", value: "20260227T120000.123Z-aaaaaaaaaaaaaaaa", want: true},
		{name: "artifact name with dashes", value: "plan/task-003-fix-1", want: false},
		{name: "uppercase hex not allowed", value: "20260227T120000Z-AAAAAAAAAAAAAAAA", want: false},
		{name: "missing timestamp", value: "just-a-name", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeRef(tc.value); got != tc.want {
				t.Fatalf("looksLikeRef(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
