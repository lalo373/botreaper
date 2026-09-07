// Package setup implements the interactive `botreaper setup` wizard.
// Ports hermes_cli/setup.py + setup_quick.py + setup_summary.py +
// model_setup_flows.py + setup_terminal.py + setup_platforms.py (messaging
// section): identical chrome (magenta banner, cyan ◆ headers, yellow
// prompts), identical prompt/validation strings, same section order.
// Deviations from Python are documented on Run: no curses (numbered-list
// fallback format everywhere), no OAuth device flows (key-based auth), and
// the catalog covers the providers this runtime implements.
package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// In and Out are swapped in tests (piped stdin, captured stdout).
var (
	In  io.Reader = os.Stdin
	Out io.Writer = os.Stdout
)

var reader *bufio.Reader
var readerFor io.Reader

func line() string {
	if reader == nil || readerFor != In {
		reader = bufio.NewReader(In)
		readerFor = In
	}
	// A pty EOF condition surfaces as repeated (0, nil) reads rather than
	// io.EOF; cap consecutive empties so EOF still falls back to defaults
	// instead of spinning forever.
	for empty := 0; ; empty++ {
		s, err := reader.ReadString('\n')
		if len(s) > 0 {
			// Strip bracketed-paste markers like the Python prompt().
			s = strings.ReplaceAll(s, "\x1b[200~", "")
			s = strings.ReplaceAll(s, "\x1b[201~", "")
			return strings.TrimSpace(s)
		}
		if err != nil || empty >= 50 {
			panic(errEOF)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type errCancel struct{ msg string }

func (e *errCancel) Error() string { return e.msg }

var (
	errEOF       = &errCancel{"EOF"}
	errCancelled = &errCancel{"cancelled"}
)

// Colors mirror the Python palette; disabled under NO_COLOR, dumb
// terminals, or non-TTY output (char-device check, no extra dependency).
func colored() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if f, ok := Out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			return fi.Mode()&os.ModeCharDevice != 0
		}
	}
	return false
}

const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cMagenta = "\033[35m"
	cCyan    = "\033[36m"
	cYellow  = "\033[33m"
	cGreen   = "\033[32m"
	cRed     = "\033[31m"
	cDim     = "\033[2m"
)

func paint(code, s string) string {
	if !colored() {
		return s
	}
	return code + s + cReset
}

func emit(format string, args ...any) {
	fmt.Fprintf(Out, format+"\n", args...)
}

// Banner prints the magenta setup box.
func Banner(title string, body ...string) {
	emit("")
	emit(paint(cMagenta, "┌─────────────────────────────────────────────────────────┐"))
	emit(paint(cMagenta, "│") + " " + padVisible(paint(cBold, title), 54) + paint(cMagenta, "│"))
	for _, b := range body {
		emit(paint(cMagenta, "│") + " " + padVisible(b, 54) + paint(cMagenta, "│"))
	}
	emit(paint(cMagenta, "└─────────────────────────────────────────────────────────┘"))
}

// padVisible pads s to width w counting only visible runes (ANSI codes
// don't advance the cursor).
func padVisible(s string, w int) string {
	n := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		n++
	}
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// Header prints a cyan ◆ section header (gap adds the leading blank pair).
func Header(title string, gap bool) {
	if gap {
		emit("")
	}
	emit("")
	emit(paint(cCyan, paint(cBold, "◆ "+title)))
}

// Success / Info / Warning / Error mirror print_success/_info/print_warning.
func Success(format string, args ...any) { emit(paint(cGreen, fmt.Sprintf(format, args...))) }
func Info(format string, args ...any)    { emit(paint(cDim, fmt.Sprintf(format, args...))) }
func Warning(format string, args ...any) { emit(paint(cYellow, fmt.Sprintf(format, args...))) }
func Error(format string, args ...any)   { emit(paint(cRed, fmt.Sprintf(format, args...))) }

// Rule prints the dim separator used after sections.
func Rule() { emit(paint(cDim, strings.Repeat("─", 60))) }

// readLine reads one line; ok=false on EOF (callers fall back to defaults,
// mirroring Python's EOFError handling).
func readLine() (s string, ok bool) {
	ok = true
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	s = line()
	return s, true
}

// Prompt asks a free-text question. Format is verbatim Python:
// "question [default]:" in yellow, plain "question:" without.
func Prompt(question, def string) string {
	if def != "" {
		fmt.Fprintf(Out, "%s ", paint(cYellow, question+" ["+def+"]"+":"))
	} else {
		fmt.Fprintf(Out, "%s ", paint(cYellow, question+":"))
	}
	if s, ok := readLine(); ok && s != "" {
		return s
	}
	return def
}

// PromptSecret is Prompt without a default and without echoing the value
// back. Terminal masking (getpass) is unavailable on plain pipes; on TTYs
// the line still shows while typing — a documented limitation.
func PromptSecret(question string) string {
	fmt.Fprintf(Out, "%s ", paint(cYellow, question+":"))
	s, _ := readLine()
	return s
}

// PromptYesNo mirrors prompt_yes_no: "[Y/n]" / "[y/N]", y/yes/n/no parse,
// "Please enter 'y' or 'n'" loop, EOF returns the default.
func PromptYesNo(question string, def bool) bool {
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	for {
		fmt.Fprintf(Out, "%s ", paint(cYellow, question+" "+hint+":"))
		s, ok := readLine()
		if !ok {
			return def
		}
		switch strings.ToLower(s) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
		Error("Please enter 'y' or 'n'")
	}
}

// PromptChoice mirrors the numbered-list fallback (curses is unavailable in
// Go): rows with "→ N." marker on the default, "Choice [1-N] (def): " line,
// "Please enter 1-N" / "Please enter a number" validation.
func PromptChoice(title string, choices []string, def int) int {
	emit(title)
	for i, c := range choices {
		mark := " "
		if i == def {
			mark = "→"
		}
		emit("  %s %d. %s", mark, i+1, c)
	}
	for {
		fmt.Fprintf(Out, "Choice [1-%d] (%d): ", len(choices), def+1)
		s, ok := readLine()
		if !ok {
			return def
		}
		if s == "" {
			Info("  Skipped (keeping current)")
			return def
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			Error("Please enter a number")
			continue
		}
		if n < 1 || n > len(choices) {
			Error("Please enter 1-%d", len(choices))
			continue
		}
		emit("")
		return n - 1
	}
}
