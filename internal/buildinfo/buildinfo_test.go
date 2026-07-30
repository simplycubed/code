package buildinfo

import "testing"

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestResolveVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		linkVersion  string
		buildVersion string
		want         string
	}{
		{
			name:         "ldflags override build info",
			linkVersion:  "v0.1.0",
			buildVersion: "v0.2.0",
			want:         "0.1.0",
		},
		{
			name:         "module version is used for go install builds",
			linkVersion:  defaultVersion,
			buildVersion: "v0.1.0",
			want:         "0.1.0",
		},
		{
			name:         "devel build keeps dev version",
			linkVersion:  defaultVersion,
			buildVersion: "(devel)",
			want:         defaultVersion,
		},
		{
			name:         "empty versions fall back to default",
			linkVersion:  "",
			buildVersion: "",
			want:         defaultVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveVersion(tt.linkVersion, tt.buildVersion); got != tt.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", tt.linkVersion, tt.buildVersion, got, tt.want)
			}
		})
	}
}
