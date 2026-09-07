package types

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
)

// Binary frame header for hot-path frames (pty bytes, bulk token batches):
// [v:1][kind:1][seq:8][epoch:8][sess_len:2][sess][type_len:1][type][payload]
// Avoids base64 overhead of JSON for raw terminal output.

var (
	bufPool = sync.Pool{New: func() any { b := new(bytes.Buffer); b.Grow(4096); return b }}
	// ErrFrameTooShort is returned when a binary frame is truncated.
	ErrFrameTooShort = errors.New("types: binary frame too short")
)

// AcquireBuffer borrows a reusable buffer. Call ReleaseBuffer when done.
func AcquireBuffer() *bytes.Buffer { return bufPool.Get().(*bytes.Buffer) }

// ReleaseBuffer returns a buffer to the pool.
func ReleaseBuffer(b *bytes.Buffer) { b.Reset(); bufPool.Put(b) }

func kindByte(k FrameKind) byte {
	switch k {
	case KindRPCRequest:
		return 1
	case KindRPCResponse:
		return 2
	case KindEvent:
		return 3
	case KindPty:
		return 4
	case KindPing:
		return 5
	case KindPong:
		return 6
	}
	return 0
}

func kindFromByte(b byte) FrameKind {
	switch b {
	case 1:
		return KindRPCRequest
	case 2:
		return KindRPCResponse
	case 3:
		return KindEvent
	case 4:
		return KindPty
	case 5:
		return KindPing
	case 6:
		return KindPong
	}
	return FrameKind("")
}

// EncodeBinary serializes e into buf in binary form (zero extra allocs
// beyond buf growth).
func EncodeBinary(buf *bytes.Buffer, e *Envelope) {
	buf.WriteByte(e.V)
	buf.WriteByte(kindByte(e.Kind))
	var tmp [10]byte
	binary.LittleEndian.PutUint64(tmp[:8], e.Seq)
	buf.Write(tmp[:8])
	binary.LittleEndian.PutUint64(tmp[:8], e.Epoch)
	buf.Write(tmp[:8])
	binary.LittleEndian.PutUint16(tmp[:2], uint16(len(e.Sess)))
	buf.Write(tmp[:2])
	buf.WriteString(e.Sess)
	buf.WriteByte(byte(len(e.Type)))
	buf.WriteString(e.Type)
	buf.Write(e.Data)
}

// DecodeBinary parses a binary frame without intermediate maps.
func DecodeBinary(raw []byte) (*Envelope, error) {
	if len(raw) < 21 {
		return nil, ErrFrameTooShort
	}
	e := &Envelope{V: raw[0], Kind: kindFromByte(raw[1])}
	e.Seq = binary.LittleEndian.Uint64(raw[2:10])
	e.Epoch = binary.LittleEndian.Uint64(raw[10:18])
	slen := int(binary.LittleEndian.Uint16(raw[18:20]))
	if len(raw) < 20+slen+1 {
		return nil, ErrFrameTooShort
	}
	e.Sess = string(raw[20 : 20+slen])
	off := 20 + slen
	tlen := int(raw[off])
	off++
	if len(raw) < off+tlen {
		return nil, ErrFrameTooShort
	}
	e.Type = string(raw[off : off+tlen])
	off += tlen
	if off < len(raw) {
		cp := make([]byte, len(raw)-off)
		copy(cp, raw[off:])
		e.Data = cp
	}
	return e, nil
}
