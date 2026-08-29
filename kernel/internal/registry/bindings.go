package registry

import (
	"encoding/json"
	"os"
)

type BindingRecord struct {
	Capability string `json:"capability"`
	Major      int    `json:"major"`
	Service    string `json:"service"`
	Authority  string `json:"authority"`
	Provider   string `json:"provider,omitempty"`
}
type BindingFile struct {
	Bindings []BindingRecord `json:"bindings"`
}

func LoadBindings(path string) (BindingFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return BindingFile{}, err
	}
	var f BindingFile
	err = json.Unmarshal(b, &f)
	return f, err
}
