package sandbox

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/crypto/ssh"
)

// SSHBackend mirrors environments/ssh.py over golang.org/x/crypto/ssh.
type SSHBackend struct {
	User    string
	Addr    string
	KeyPath string
	client  *ssh.Client
}

// NewSSH builds an SSH backend (lazy dial on first Exec).
func NewSSH(user, addr, keyPath string) *SSHBackend {
	return &SSHBackend{User: user, Addr: addr, KeyPath: keyPath}
}

// Name implements Backend.
func (b *SSHBackend) Name() string { return "ssh" }

// Available implements Backend.
func (b *SSHBackend) Available() bool { return b.User != "" && b.Addr != "" }

func (b *SSHBackend) dial() (*ssh.Client, error) {
	if b.client != nil {
		return b.client, nil
	}
	var auth []ssh.AuthMethod
	if b.KeyPath != "" {
		raw, err := os.ReadFile(b.KeyPath)
		if err != nil {
			return nil, err
		}
		key, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(key))
	} else if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		return nil, errors.New("sandbox: ssh agent forwarding requires explicit key in this build")
	} else {
		return nil, errors.New("sandbox: ssh needs KeyPath")
	}
	cfg := &ssh.ClientConfig{User: b.User, Auth: auth, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	cl, err := ssh.Dial("tcp", b.Addr, cfg)
	if err != nil {
		return nil, err
	}
	b.client = cl
	return cl, nil
}

// Exec runs cmd remotely, streaming both pipes.
func (b *SSHBackend) Exec(ctx context.Context, cmd string, out chan<- Chunk) (int, error) {
	cl, err := b.dial()
	if err != nil {
		return 127, err
	}
	sess, err := cl.NewSession()
	if err != nil {
		return 127, err
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return 127, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return 127, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pump(ctx, io.MultiReader(stdout), "stdout", out)
	}()
	go pump(ctx, stderr, "stderr", out)
	runErr := sess.Run(cmd)
	<-done
	if runErr != nil {
		return 1, nil
	}
	_ = ctx
	return 0, nil
}
