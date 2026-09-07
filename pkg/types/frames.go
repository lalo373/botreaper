package types

// WebSocket frame contracts. JSON-RPC 2.0 over ws; no HTTP polling.
// Mirrors tui_gateway/ws.py + apps/shared json-rpc-gateway.ts:
// ping 15s / kill 45s, replay_epoch + per-session seq resume.

const ProtocolVersion uint8 = 1

type FrameKind string

const (
	KindRPCRequest  FrameKind = "req"
	KindRPCResponse FrameKind = "res"
	KindEvent       FrameKind = "ev"
	KindPty         FrameKind = "pty"
	KindPing        FrameKind = "ping"
	KindPong        FrameKind = "pong"
)

// Envelope is the single unit on /api/ws. Data holds raw JSON
// (goccy/go-json) to avoid re-encode allocs on fan-out.
type Envelope struct {
	V     uint8     `json:"v"`
	Kind  FrameKind `json:"k"`
	Seq   uint64    `json:"seq"`
	Epoch uint64    `json:"epoch"`
	Sess  string    `json:"sess"`
	Type  string    `json:"t"`
	Data  []byte    `json:"d,omitempty"`
}

// Event types (S=server->client, C=client->server).
const (
	EvTokenDelta      = "token.delta"
	EvReasoningDelta  = "reasoning.delta"
	EvThinkingDelta   = "thinking.delta"
	EvToolStart       = "tool.start"
	EvToolProgress    = "tool.progress"
	EvToolEnd         = "tool.end"
	EvPtyStdout       = "pty.stdout"
	EvPtyStderr       = "pty.stderr"
	EvSessionSnapshot = "session.snapshot"
	EvSessionPatch    = "session.patch"
	EvChangeEvents    = "change_events"
	EvMessageAppended = "message.appended"
	EvApprovalRequest = "approval_request"
	EvCronFired       = "cron.fired"
	EvCronAcked       = "cron.acked"
	EvGatewayStatus   = "gateway.status"
	EvGatewayHB       = "gateway.heartbeat"
	EvPromptSubmit    = "prompt.submit"
	EvPromptCancel    = "prompt.cancel"
	EvSlashExecute    = "slash.execute"
	EvEventsSince     = "session.events.since"
	EvBrowserFrame    = "browser.frame"
	EvVoiceChunk      = "voice.chunk"
)
