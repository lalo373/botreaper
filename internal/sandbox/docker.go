package sandbox

import (
	"context"
	"os/exec"
	"sync"
)

// containerBackend shares Exec logic for docker/singularity CLIs.
type containerBackend struct {
	name string
	bin  string
	args []string
}

func (b *containerBackend) Name() string { return b.name }

func (b *containerBackend) Available() bool {
	_, err := exec.LookPath(b.bin)
	return err == nil
}

func (b *containerBackend) Exec(ctx context.Context, cmd string, out chan<- Chunk) (int, error) {
	argv := append(append([]string{}, b.args...), cmd)
	c := exec.CommandContext(ctx, b.bin, argv...)
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

// NewDocker mirrors environments/docker.py (container exec via CLI).
func NewDocker(container string) Backend {
	return &containerBackend{name: "docker", bin: "docker", args: []string{"exec", "-i", container, "/bin/sh", "-c"}}
}

// NewSingularity mirrors environments/singularity.py.
func NewSingularity(image string) Backend {
	return &containerBackend{name: "singularity", bin: "singularity", args: []string{"exec", image, "/bin/sh", "-c"}}
}
