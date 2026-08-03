package main

import (
	"errors"
	"net"
	"testing"
	"time"
)

type retryPacketConn struct {
	writes [][]byte
	reads  []retryRead
}

type retryRead struct {
	data []byte
	err  error
}

func (r *retryPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if len(r.reads) == 0 {
		return 0, nil, errors.New("unexpected read")
	}
	next := r.reads[0]
	r.reads = r.reads[1:]
	if next.err != nil {
		return 0, nil, next.err
	}
	return copy(b, next.data), fakeAddr("target"), nil
}

func (r *retryPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	r.writes = append(r.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (r *retryPacketConn) Close() error                     { return nil }
func (r *retryPacketConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (r *retryPacketConn) SetDeadline(time.Time) error      { return nil }
func (r *retryPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (r *retryPacketConn) SetWriteDeadline(time.Time) error { return nil }

func TestReliableEnvelopeRoundTrip(t *testing.T) {
	encoded, err := encodeReliableRequest("req-1", []byte("hello"))
	if err != nil {
		t.Fatalf("encodeReliableRequest() error = %v", err)
	}
	requestID, payload, err := decodeReliableRequest(encoded)
	if err != nil {
		t.Fatalf("decodeReliableRequest() error = %v", err)
	}
	if requestID != "req-1" {
		t.Fatalf("requestID = %q", requestID)
	}
	if string(payload) != "hello" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestExchangeReliablePayloadRetriesWithSameRequestID(t *testing.T) {
	response, err := encodeReliableResponse("req-1", []byte("reply: hello"))
	if err != nil {
		t.Fatalf("encodeReliableResponse() error = %v", err)
	}
	conn := &retryPacketConn{
		reads: []retryRead{
			{err: errors.New("timeout")},
			{data: response},
		},
	}

	payload, _, attempts, err := exchangeReliablePayload(conn, []byte("hello"), ReliabilityConfig{
		MaxRetries:      3,
		Timeout:         time.Millisecond,
		MaxPayloadBytes: 1000,
	}, "req-1")
	if err != nil {
		t.Fatalf("exchangeReliablePayload() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if string(payload) != "reply: hello" {
		t.Fatalf("payload = %q", payload)
	}
	if len(conn.writes) != 2 {
		t.Fatalf("writes = %d, want 2", len(conn.writes))
	}
	for _, write := range conn.writes {
		requestID, _, err := decodeReliableRequest(write)
		if err != nil {
			t.Fatalf("decodeReliableRequest(write) error = %v", err)
		}
		if requestID != "req-1" {
			t.Fatalf("retry requestID = %q, want req-1", requestID)
		}
	}
}

func TestExchangeReliablePayloadFailsAfterMaxRetries(t *testing.T) {
	conn := &retryPacketConn{
		reads: []retryRead{
			{err: errors.New("timeout 1")},
			{err: errors.New("timeout 2")},
		},
	}

	_, _, attempts, err := exchangeReliablePayload(conn, []byte("hello"), ReliabilityConfig{
		MaxRetries:      1,
		Timeout:         time.Millisecond,
		MaxPayloadBytes: 1000,
	}, "req-1")
	if err == nil {
		t.Fatal("exchangeReliablePayload() error = nil, want retry failure")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
