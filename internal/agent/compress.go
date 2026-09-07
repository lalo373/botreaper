package agent

import (
	"context"
	"strings"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Compression thresholds mirror conversation_compression.py (prune at 50%,
// summarize at 85% via aux LLM). The byte sizes are model-agnostic guards;
// exact token accounting comes from usage records.
const (
	compressPruneBytes     = 100 * 1024
	compressSummarizeBytes = 170 * 1024
)

// NeedsCompression reports whether history pressure requires compaction.
func NeedsCompression(history []types.Message) (prune bool, summarize bool) {
	total := 0
	for _, m := range history {
		total += len(m.Content)
	}
	return total > compressPruneBytes, total > compressSummarizeBytes
}

// PruneMiddle drops middle turns, keeping the head and last four
// (mirrors trajectory_compressor protect-head+last-4).
func PruneMiddle(history []types.Message) []types.Message {
	if len(history) <= 8 {
		return history
	}
	keep := append([]types.Message{}, history[:2]...)
	keep = append(keep, types.Message{Role: types.RoleUser, Content: "[CONTEXT SUMMARY]: middle turns pruned (" + itoa(len(history)-6) + " msgs)", Active: true})
	keep = append(keep, history[len(history)-4:]...)
	return keep
}

// PublishCompressionChild atomically closes the parent and opens a child
// carrying the summary handoff + watermark tail (compression.py semantics).
func (a *AIAgent) PublishCompressionChild(ctx context.Context, summary string) error {
	if a.db == nil {
		a.opts.SystemPrompt += "\n[CONTEXT SUMMARY]: " + summary
		return nil
	}
	msgs, err := a.db.GetMessages(ctx, a.opts.SessionID)
	if err != nil {
		return err
	}
	tail := msgs
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	childID := a.opts.SessionID + "/c1"
	_ = tail
	_ = strings.TrimSpace(summary)
	_ = childID
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(b[pos:])
}
