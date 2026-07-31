package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/masque-go"
)

type fakePacketConn struct {
	written      []byte
	readData     []byte
	readAddr     net.Addr
	readDeadline time.Time
	closed       bool
}

func (f *fakePacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	return copy(b, f.readData), f.readAddr, nil
}

func (f *fakePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	f.written = append(f.written[:0], p...)
	return len(p), nil
}

func (f *fakePacketConn) Close() error {
	f.closed = true
	return nil
}

func (f *fakePacketConn) LocalAddr() net.Addr               { return fakeAddr("local") }
func (f *fakePacketConn) RemoteAddr() net.Addr              { return fakeAddr("remote") }
func (f *fakePacketConn) SetDeadline(time.Time) error       { return nil }
func (f *fakePacketConn) SetReadDeadline(t time.Time) error { f.readDeadline = t; return nil }
func (f *fakePacketConn) SetWriteDeadline(time.Time) error  { return nil }

type fakeAddr string

func (f fakeAddr) Network() string { return "fake" }
func (f fakeAddr) String() string  { return string(f) }

func TestSendPayloadWritesPayloadToTunnel(t *testing.T) {
	conn := &fakePacketConn{}

	if err := sendPayload(conn, []byte("hello")); err != nil {
		t.Fatalf("sendPayload() error = %v", err)
	}

	if string(conn.written) != "hello" {
		t.Fatalf("written payload = %q, want %q", conn.written, "hello")
	}
}

func TestReadResponseSetsDeadlineAndReturnsPayload(t *testing.T) {
	conn := &fakePacketConn{
		readData: []byte("reply: hello"),
		readAddr: fakeAddr("target"),
	}

	payload, addr, err := readResponse(conn, 2*time.Second, 64)
	if err != nil {
		t.Fatalf("readResponse() error = %v", err)
	}

	if string(payload) != "reply: hello" {
		t.Fatalf("payload = %q, want %q", payload, "reply: hello")
	}
	if addr.String() != "target" {
		t.Fatalf("addr = %q, want %q", addr.String(), "target")
	}
	if conn.readDeadline.IsZero() {
		t.Fatal("read deadline was not set")
	}
}

func TestConnectTunnelRejectsNonSuccessResponseAndClosesConn(t *testing.T) {
	conn := &fakePacketConn{}
	cfg := config{
		uriTemplate:    "https://proxy.example/masque?h={target_host}&p={target_port}",
		target:         "192.168.123.100:7400",
		readBufferSize: 64,
		timeout:        time.Second,
	}

	_, err := connectTunnel(context.Background(), cfg, func(*masque.Request) (tunnelConn, *http.Response, error) {
		return conn, &http.Response{StatusCode: http.StatusServiceUnavailable}, nil
	})

	if err == nil {
		t.Fatal("connectTunnel() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "HTTP status 503") {
		t.Fatalf("error = %q, want HTTP status 503", err)
	}
	if !conn.closed {
		t.Fatal("connection was not closed")
	}
}

func TestConnectTunnelWrapsDialError(t *testing.T) {
	cfg := config{
		uriTemplate:    "https://proxy.example/masque?h={target_host}&p={target_port}",
		target:         "192.168.123.100:7400",
		readBufferSize: 64,
		timeout:        time.Second,
	}
	dialErr := errors.New("dial failed")

	_, err := connectTunnel(context.Background(), cfg, func(*masque.Request) (tunnelConn, *http.Response, error) {
		return nil, nil, dialErr
	})

	if !errors.Is(err, dialErr) {
		t.Fatalf("connectTunnel() error = %v, want to wrap %v", err, dialErr)
	}
}
