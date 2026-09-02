package main

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

type Transcript struct {
	Frames []Frame
	Result RunResult
}

func (t Transcript) Bytes() []byte {
	var b bytes.Buffer
	for _, f := range t.Frames {
		line, _ := json.Marshal(f)
		b.Write(line)
		b.WriteByte('\n')
	}
	b.WriteString("== result: ")
	b.WriteString(t.Result.Status)
	b.WriteString(" ==\n")
	return b.Bytes()
}

func runProvider(ctx context.Context, prov Provider, spec RunSpec, mirror chan<- any) Transcript {
	frames := make(chan Frame, 16)
	result := make(chan RunResult, 1)
	go func() { result <- prov.Run(ctx, spec, frames) }()
	tr := Transcript{}
	for f := range frames {
		tr.Frames = append(tr.Frames, f)
		select {
		case mirror <- f:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	tr.Result = <-result
	close(mirror)
	return tr
}
