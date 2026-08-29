// org.vibe.blob — content-addressed byte store. Knows nothing about Task, Agent,
// Diff or Session; it maps bytes <-> blob://sha256/<hex> URIs.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

const uriPrefix = "blob://sha256/"

type store struct{ root string }

func (s *store) objectsDir() string { return filepath.Join(s.root, "objects", "sha256") }
func (s *store) pathFor(hexsum string) string {
	return filepath.Join(s.objectsDir(), hexsum[:2], hexsum)
}

func hexFromURI(uri string) (string, bool) {
	if !strings.HasPrefix(uri, uriPrefix) {
		return "", false
	}
	h := strings.TrimPrefix(uri, uriPrefix)
	if len(h) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", false
	}
	return h, true
}

type putRequest struct {
	ContentBase64 string `json:"content_base64"`
}
type putResponse struct {
	URI     string `json:"uri"`
	SHA256  string `json:"sha256"`
	Size    int    `json:"size"`
	Existed bool   `json:"existed"`
}
type getRequest struct {
	URI string `json:"uri"`
}
type getResponse struct {
	ContentBase64 string `json:"content_base64"`
	Size          int    `json:"size"`
}
type statResponse struct {
	Exists bool `json:"exists"`
	Size   int  `json:"size"`
}

func putHandler(s *store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q putRequest
		if json.Unmarshal(e.Payload, &q) != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "bad request"}
		}
		raw, derr := base64.StdEncoding.DecodeString(q.ContentBase64)
		if derr != nil {
			return nil, &protocol.Error{Code: "INVALID", Message: "content_base64 is not valid base64"}
		}
		sum := sha256.Sum256(raw)
		hexsum := hex.EncodeToString(sum[:])
		dst := s.pathFor(hexsum)

		var resp putResponse
		err := fencing.WithWriteFence(e, func() error {
			if fi, statErr := os.Stat(dst); statErr == nil {
				resp = putResponse{URI: uriPrefix + hexsum, SHA256: hexsum, Size: int(fi.Size()), Existed: true}
				return nil
			}
			if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
				return mkErr
			}
			tmp := dst + ".tmp"
			f, oErr := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if oErr != nil {
				return oErr
			}
			_, wErr := f.Write(raw)
			if wErr == nil {
				wErr = f.Sync()
			}
			cErr := f.Close()
			if wErr != nil {
				_ = os.Remove(tmp)
				return wErr
			}
			if cErr != nil {
				_ = os.Remove(tmp)
				return cErr
			}
			if rErr := os.Rename(tmp, dst); rErr != nil {
				_ = os.Remove(tmp)
				return rErr
			}
			resp = putResponse{URI: uriPrefix + hexsum, SHA256: hexsum, Size: len(raw), Existed: false}
			return nil
		})
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error(), Retryable: true}
		}
		return resp, nil
	}
}

func getHandler(s *store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q getRequest
		_ = json.Unmarshal(e.Payload, &q)
		h, ok := hexFromURI(q.URI)
		if !ok {
			return nil, &protocol.Error{Code: "INVALID", Message: "uri must be blob://sha256/<64-hex>"}
		}
		raw, err := os.ReadFile(s.pathFor(h))
		if os.IsNotExist(err) {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: "no such blob"}
		}
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error()}
		}
		return getResponse{ContentBase64: base64.StdEncoding.EncodeToString(raw), Size: len(raw)}, nil
	}
}

func statHandler(s *store) pluginhost.Handler {
	return func(e protocol.Envelope) (any, *protocol.Error) {
		var q getRequest
		_ = json.Unmarshal(e.Payload, &q)
		h, ok := hexFromURI(q.URI)
		if !ok {
			return nil, &protocol.Error{Code: "INVALID", Message: "uri must be blob://sha256/<64-hex>"}
		}
		fi, err := os.Stat(s.pathFor(h))
		if os.IsNotExist(err) {
			return statResponse{Exists: false}, nil
		}
		if err != nil {
			return nil, &protocol.Error{Code: "IO", Message: err.Error()}
		}
		return statResponse{Exists: true, Size: int(fi.Size())}, nil
	}
}

func main() {
	root := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(root, 0o755); err != nil {
		panic(err)
	}
	s := &store{root: root}
	h := pluginhost.New("org.vibe.blob", "1.0.0", "")
	h.HandleContextCommand("blob.put", 1, func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		return putHandler(s)(e)
	})
	h.HandleQuery("blob.get", 1, getHandler(s))
	h.HandleQuery("blob.stat", 1, statHandler(s))
	_ = h.Serve()
}
