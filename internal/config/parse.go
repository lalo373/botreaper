package config

import "strings"

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func cutKV(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:idx])
	v := strings.TrimSpace(line[idx+1:])
	v = strings.Trim(v, `"'`)
	return k, v, k != ""
}

// cutEnv splits dotenv lines (KEY=value, tolerating `export ` prefixes and
// single/double quotes). config.yaml keeps cutKV (`key: value`); .env must
// use this — values like tokens routinely contain colons.
func cutEnv(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	rest := line
	if strings.HasPrefix(rest, "export ") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "export "))
	}
	idx := strings.IndexByte(rest, '=')
	if idx < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(rest[:idx])
	v := strings.TrimSpace(rest[idx+1:])
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		v = v[1 : len(v)-1]
	}
	if k == "" || strings.ContainsAny(k, " \t:#") {
		return "", "", false
	}
	return k, v, true
}

func atoi(s string, def int) int {
	n := 0
	neg := false
	i := 0
	if len(s) > 0 && s[0] == '-' {
		neg = true
		i = 1
	}
	digits := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return def
		}
		n = n*10 + int(s[i]-'0')
		digits++
	}
	if digits == 0 {
		return def
	}
	if neg {
		return -n
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
