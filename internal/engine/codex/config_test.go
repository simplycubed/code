package codex

import (
	"os"
	"strings"
	"testing"
)

func TestWriteConfigRendersProviderAndNeverTheKeyValue(t *testing.T) {
	home := t.TempDir()
	path, err := WriteConfig(home, ProviderConfig{
		Model:   "gpt-5.4",
		BaseURL: "https://example-resource.openai.azure.com/openai/v1",
		EnvKey:  "SIMPLYCUBED_AZURE_OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)

	for _, want := range []string{
		`model = "gpt-5.4"`,
		`model_provider = "azure"`,
		`[model_providers.azure]`,
		`base_url = "https://example-resource.openai.azure.com/openai/v1"`,
		`env_key = "SIMPLYCUBED_AZURE_OPENAI_API_KEY"`,
		`wire_api = "responses"`, // default applied
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("config missing %q\n---\n%s", want, got)
		}
	}

	// The file references the key by name only. A value must never be written.
	// (A real key would look like a long token; assert the assignment form is a
	// bare name, not name=<secret>.)
	if strings.Contains(got, "SIMPLYCUBED_AZURE_OPENAI_API_KEY=") {
		t.Fatal("config appears to contain a key value, not just the env-var name")
	}
}

func TestWriteConfigRequiresFields(t *testing.T) {
	cases := []ProviderConfig{
		{BaseURL: "u", EnvKey: "K"}, // no Model
		{Model: "m", EnvKey: "K"},   // no BaseURL
		{Model: "m", BaseURL: "u"},  // no EnvKey
	}
	for i, c := range cases {
		if _, err := WriteConfig(t.TempDir(), c); err == nil {
			t.Fatalf("case %d: expected an error for incomplete config", i)
		}
	}
}
