package masque

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"testing"
)

func TestShouldLogRequestStreamErrorSuppressesEOF(t *testing.T) {
	if shouldLogRequestStreamError(io.EOF) {
		t.Fatal("io.EOF should be treated as a normal request stream close")
	}
}

func TestShouldLogRequestStreamErrorKeepsRealErrors(t *testing.T) {
	if !shouldLogRequestStreamError(errors.New("capsule parse failed")) {
		t.Fatal("real request stream errors should still be logged")
	}
}

func TestShouldLogProxyReceiveErrorSuppressesClosedConnection(t *testing.T) {
	tests := []error{
		net.ErrClosed,
		errors.New("read udp 192.168.1.45:63675->192.168.1.45:7400: use of closed network connection"),
	}
	for _, err := range tests {
		if shouldLogProxyReceiveError(err, false) {
			t.Fatalf("%q should be treated as a normal UDP connection close", err)
		}
	}
}

func TestShouldLogProxyReceiveErrorKeepsRealErrors(t *testing.T) {
	if !shouldLogProxyReceiveError(errors.New("DATAGRAM frame too large"), false) {
		t.Fatal("real proxy receive errors should still be logged")
	}
}

func TestShouldLogProxyReceiveErrorSuppressesProxyClose(t *testing.T) {
	if shouldLogProxyReceiveError(errors.New("target read failed"), true) {
		t.Fatal("errors during proxy shutdown should not be logged as request failures")
	}
}

func TestProxyLifecycleLogs(t *testing.T) {
	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer log.SetOutput(oldOutput)
	defer log.SetFlags(oldFlags)

	logTunnelAccepted("192.168.1.20", "192.168.1.45:7400", fakeAddr("192.168.1.45:63675"), fakeAddr("192.168.1.45:7400"))
	logForwardClientToTarget("192.168.1.20", "192.168.1.45:7400", 5, 30)
	logForwardTargetToClient("192.168.1.45:7400", 12)
	logTunnelClosed("192.168.1.20", "192.168.1.45:7400")

	got := buf.String()
	wants := []string{
		"accepted CONNECT-UDP tunnel client_ip=192.168.1.20 target=192.168.1.45:7400 udp_local=192.168.1.45:63675 udp_remote=192.168.1.45:7400",
		"closed CONNECT-UDP tunnel client_ip=192.168.1.20 target=192.168.1.45:7400",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q; got:\n%s", want, got)
		}
	}

	timePattern := `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}[+-]\d{2}:\d{2}`
	patterns := []string{
		`forward client->target time=` + timePattern + ` client_ip=192\.168\.1\.20 target=192\.168\.1\.45:7400 payload_bytes=5 wrapped_bytes=30`,
		`forward target->client time=` + timePattern + ` target=192\.168\.1\.45:7400 payload_bytes=12`,
	}
	for _, pattern := range patterns {
		if !regexp.MustCompile(pattern).MatchString(got) {
			t.Fatalf("log output missing pattern %q; got:\n%s", pattern, got)
		}
	}
}

type fakeAddr string

func (f fakeAddr) Network() string { return "fake" }
func (f fakeAddr) String() string  { return string(f) }
