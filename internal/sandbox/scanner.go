package sandbox

import (
	"errors"
	"strings"
)

// Scan verdicts mirror the terminal command security scanner.
type Verdict int

const (
	// Allow runs untouched.
	Allow Verdict = iota
	// NeedsApproval requires HITL before spawn.
	NeedsApproval
	// Deny refuses outright.
	Deny
)

// ScanResult explains the verdict.
type ScanResult struct {
	Verdict Verdict
	Reason  string
}

var denyPrefixes = []string{
	"rm -rf /", "rm -rf /*", ":(){:|:&};:", "mkfs.", "dd if=",
	">/dev/sda", "chmod -R 777 /", "curl | sh", "wget | sh",
}

var approvalHints = []string{
	"sudo ", "rm -rf ", "chmod ", "chown ", "iptables", "shutdown",
	"reboot", "docker ", "kubectl ", "terraform apply",
}

// ScanCommand classifies cmd before any backend spawn.
func ScanCommand(cmd string) ScanResult {
	lowered := strings.ToLower(strings.TrimSpace(cmd))
	if lowered == "" {
		return ScanResult{Verdict: Deny, Reason: "empty command"}
	}
	for _, d := range denyPrefixes {
		if strings.Contains(lowered, strings.ToLower(d)) {
			return ScanResult{Verdict: Deny, Reason: "matches deny pattern: " + d}
		}
	}
	for _, h := range approvalHints {
		if strings.Contains(lowered, strings.ToLower(h)) {
			return ScanResult{Verdict: NeedsApproval, Reason: "privileged pattern: " + h}
		}
	}
	return ScanResult{Verdict: Allow}
}

// CheckExec runs the scanner and returns a hard error on Deny.
func CheckExec(cmd string) error {
	switch r := ScanCommand(cmd); r.Verdict {
	case Deny:
		return errors.New("sandbox: denied: " + r.Reason)
	default:
		return nil
	}
}
