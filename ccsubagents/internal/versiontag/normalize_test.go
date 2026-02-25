package versiontag

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "whitespace", raw: " \t\n ", want: ""},
		{name: "none", raw: "none", want: ""},
		{name: "none mixed case", raw: " NoNe ", want: ""},
		{name: "null", raw: "null", want: ""},
		{name: "null mixed case", raw: " NuLl ", want: ""},
		{name: "already lowercase prefix", raw: "v1.2.3", want: "v1.2.3"},
		{name: "already uppercase prefix", raw: "V1.2.3", want: "v1.2.3"},
		{name: "without prefix", raw: "1.2.3", want: "v1.2.3"},
		{name: "trim then prefix", raw: "  1.2.3  ", want: "v1.2.3"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.raw)
			if got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
