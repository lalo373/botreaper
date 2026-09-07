package sandbox

import (
	"context"
	"os/exec"
	"sync"
)

// LocalBackend runs commands on the host shell. Mirrors environments/local.py.
type LocalBackend struct {
	Shell string
}

// Name implements Backend.
func (b *LocalBackend) Name() string { return "local" }

// Available implements Backend.
func (b *LocalBackend) Available() bool { return true }

// Exec streams stdout/stderr via io.Pipe-style pumps without heap retention.
func (b *LocalBackend) Exec(ctx context.Context, cmd string, out chan<- Chunk) (int, error) {
	shell := b.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	c := exec.CommandContext(ctx, shell, "-c", cmd)
	stdout, err := c.StdoutPipe()
	if err != nil {
		return 127, err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return 127, err
	}
	if err := c.Start(); err != nil {
		return 127, err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pump(ctx, stdout, "stdout", out) }()
	go func() { defer wg.Done(); pump(ctx, stderr, "stderr", out) }()
	werr := c.Wait()
	wg.Wait()
	if werr != nil {
		if ee, ok := werr.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return 1, werr
	}
	return 0, nil
}
