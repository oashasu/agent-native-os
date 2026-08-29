package contractmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

type Metadata struct {
	Contract string        `json:"contract"`
	Version  string        `json:"version"`
	Kind     protocol.Kind `json:"kind"`
}

type Catalog struct {
	ByCapability map[string]Metadata
	ByContract   map[string]Metadata
}

func Load(root string) (*Catalog, error) {
	b, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err != nil {
		return nil, err
	}
	var paths map[string]string
	if err := json.Unmarshal(b, &paths); err != nil {
		return nil, err
	}
	c := &Catalog{ByCapability: map[string]Metadata{}, ByContract: map[string]Metadata{}}
	for capKey, rel := range paths {
		sb, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, fmt.Errorf("contract %s: %w", capKey, err)
		}
		var m Metadata
		if err := json.Unmarshal(sb, &m); err != nil {
			return nil, fmt.Errorf("contract %s metadata: %w", capKey, err)
		}
		if m.Contract == "" || (m.Kind != protocol.KindCommand && m.Kind != protocol.KindQuery && m.Kind != protocol.KindEvent) {
			return nil, fmt.Errorf("contract %s missing valid contract/kind metadata", capKey)
		}
		if _, exists := c.ByContract[m.Contract]; exists {
			return nil, fmt.Errorf("duplicate contract identity %s", m.Contract)
		}
		c.ByCapability[capKey] = m
		c.ByContract[m.Contract] = m
	}
	return c, nil
}

func (c *Catalog) Metadata(contract string) (Metadata, bool) {
	if c == nil {
		return Metadata{}, false
	}
	m, ok := c.ByContract[contract]
	return m, ok
}
