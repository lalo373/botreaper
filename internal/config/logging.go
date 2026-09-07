package config

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Logger mirrors hermes_logging.py: per-component files under <home>/logs
// with PII redaction. Loggers are process-wide singletons keyed by component.
var (
	logMu   sync.Mutex
	loggers = map[string]*log.Logger{}
)

func redact(s string) string {
	// Minimal redaction: mask common secret assignments.
	for _, key := range []string{"api_key", "apikey", "token", "secret", "password"} {
		idx := strings.Index(strings.ToLower(s), key)
		if idx >= 0 {
			end := idx + len(key) + 16
			if end > len(s) {
				end = len(s)
			}
			s = s[:idx+len(key)] + "=***" + s[end:]
		}
	}
	return s
}

type redactWriter struct{ w io.Writer }

func (r redactWriter) Write(p []byte) (int, error) { return r.w.Write([]byte(redact(string(p)))) }

// ForComponent returns the logger writing to <home>/logs/<component>.log.
func ForComponent(home, component string) *log.Logger {
	logMu.Lock()
	defer logMu.Unlock()
	key := home + "\x00" + component
	if l, ok := loggers[key]; ok {
		return l
	}
	dir := filepath.Join(home, "logs")
	_ = os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, component+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o660)
	var w io.Writer = io.Discard
	if err == nil {
		w = redactWriter{f}
	}
	l := log.New(w, "", log.LstdFlags|log.Lmsgprefix)
	loggers[key] = l
	return l
}
