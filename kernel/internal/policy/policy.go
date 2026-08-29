package policy

import (
	"encoding/json"
	"fmt"
	"github.com/example/agent-native-microkernel/internal/authz"
	"os"
)

type ClientCredential struct {
	TokenSHA256 string `json:"token_sha256"`
}

type File struct {
	Grants  map[string]authz.Grant      `json:"grants"`
	Clients map[string]ClientCredential `json:"clients,omitempty"`
}

func Load(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	if err := f.Validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f File) Validate() error {
	for identity, c := range f.Clients {
		if identity == "" {
			return fmt.Errorf("client identity must not be empty")
		}
		if len(c.TokenSHA256) != 64 {
			return fmt.Errorf("client %s must define a 64-character token_sha256", identity)
		}
		if _, ok := f.Grants[identity]; !ok {
			return fmt.Errorf("client %s has credentials but no authorization grant", identity)
		}
	}
	return nil
}

func (f File) ClientTokens() map[string]string {
	out := make(map[string]string, len(f.Clients))
	for identity, c := range f.Clients {
		out[identity] = c.TokenSHA256
	}
	return out
}
