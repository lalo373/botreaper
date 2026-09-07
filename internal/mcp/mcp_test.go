package mcp

import (
	"context"
	"testing"
)

func TestClientBuild(t *testing.T) {
	c := NewClient(ServerConfig{Name: "demo", Transport: TransportStdio, Command: "true"})
	if c == nil {
		t.Fatal("nil client")
	}
	bad := NewClient(ServerConfig{Name: "bad", Transport: TransportStdio})
	if err := bad.Start(context.Background()); err == nil {
		_ = bad.Close()
		t.Fatal("stdio without command started")
	}
}
