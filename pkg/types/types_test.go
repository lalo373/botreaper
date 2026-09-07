package types

import (
	"bytes"
	"testing"
)

func TestValidateAlternation(t *testing.T) {
	ok := []Message{{Role: RoleUser}, {Role: RoleAssistant}, {Role: RoleUser}}
	if !ValidateAlternation(ok) {
		t.Fatal("valid alternation rejected")
	}
	bad := []Message{{Role: RoleUser}, {Role: RoleUser}}
	if ValidateAlternation(bad) {
		t.Fatal("invalid alternation accepted")
	}
	tools := []Message{{Role: RoleTool}, {Role: RoleTool}}
	if !ValidateAlternation(tools) {
		t.Fatal("consecutive tool messages must be allowed")
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	if got := SanitizeFTSQuery(""); got != `""` {
		t.Fatalf("empty query = %q", got)
	}
	got := SanitizeFTSQuery("hello world-foo bar.baz")
	if got == "" {
		t.Fatal("empty sanitized query")
	}
	long := string(make([]byte, 3000))
	for i := range []byte(long) {
		_ = i
	}
	b := make([]byte, 3000)
	for i := range b {
		b[i] = 'a'
	}
	if got := SanitizeFTSQuery(string(b)); len(got) > 2050 {
		t.Fatalf("query not capped: %d", len(got))
	}
}

func TestBinaryRoundTrip(t *testing.T) {
	e := &Envelope{V: 1, Kind: KindPty, Seq: 42, Epoch: 7, Sess: "s1", Type: EvPtyStdout, Data: []byte("hello")}
	buf := AcquireBuffer()
	EncodeBinary(buf, e)
	raw := append([]byte{}, buf.Bytes()...)
	ReleaseBuffer(buf)
	back, err := DecodeBinary(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Seq != 42 || back.Epoch != 7 || back.Sess != "s1" || back.Type != EvPtyStdout || !bytes.Equal(back.Data, []byte("hello")) {
		t.Fatalf("round trip mismatch: %+v", back)
	}
	if _, err := DecodeBinary([]byte{1, 2}); err != ErrFrameTooShort {
		t.Fatalf("expected ErrFrameTooShort, got %v", err)
	}
}

func BenchmarkEncodeBinary(b *testing.B) {
	e := &Envelope{V: 1, Kind: KindEvent, Seq: 1, Epoch: 1, Sess: "bench", Type: EvTokenDelta, Data: []byte("tok")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := AcquireBuffer()
		EncodeBinary(buf, e)
		ReleaseBuffer(buf)
	}
}
