package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateClientRequiresGrant(t *testing.T) {
	f := File{Clients: map[string]ClientCredential{"local-cli": {TokenSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	if err := f.Validate(); err == nil {
		t.Fatal("expected client credential without grant to fail validation")
	}
}

func TestLoadAndClientTokens(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(p, []byte(`{"clients":{"cli":{"token_sha256":"2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b"}},"grants":{"cli":{"capabilities":["demo.echo@1"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.ClientTokens()["cli"]; got != "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b" {
		t.Fatalf("unexpected token %q", got)
	}
}
