// Package sandbox implements streamed execution backends.
// Ports tools/environments/ (local/docker/ssh/modal/daytona/singularity).
package sandbox

import (
	"bufio"
	"context"
	"io"
)

// Chunk is one stdout/stderr fragment streamed to WebSocket PTY frames.
type Chunk struct {
	Stream string // "stdout" | "stderr"
	Data   []byte
}

// Backend executes commands with zero-copy output streaming.
type Backend interface {
	Name() string
	Exec(ctx context.Context, cmd string, out chan<- Chunk) (int, error)
	Available() bool
}

// PumpError caps how much stderr text is retained for error summaries
// (mirrors the 2048ch tool error cap without heap-hoarding full logs).
const PumpErrorCap = 2048

// pump copies r to out as chunks until EOF or ctx cancel. It never buffers
// the whole stream: callers forward chunks straight to WS binary frames.
func pump(ctx context.Context, r io.Reader, stream string, out chan<- Chunk) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 32*1024), 256*1024)
	for sc.Scan() {
		line := append([]byte{}, sc.Bytes()...)
		line = append(line, '\n')
		select {
		case <-ctx.Done():
			return
		case out <- Chunk{Stream: stream, Data: line}:
		}
	}
}
