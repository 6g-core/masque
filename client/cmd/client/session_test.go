package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"
)

func TestRunSessionLoopKeepsTunnelOpenForSendThenClose(t *testing.T) {
	response, err := encodeReliableResponse("req-1", []byte("reply: hello"))
	if err != nil {
		t.Fatalf("encodeReliableResponse() error = %v", err)
	}
	conn := &fakePacketConn{
		readData: response,
		readAddr: fakeAddr("target"),
	}
	input := strings.NewReader(
		`{"op":"send","payload":"hello"}` + "\n" +
			`{"op":"close"}` + "\n",
	)
	var output bytes.Buffer

	err = runSessionLoop(conn, input, &output, ReliabilityConfig{
		MaxRetries:      3,
		Timeout:         time.Millisecond,
		MaxPayloadBytes: 1000,
	}, func() string { return "req-1" })
	if err != nil {
		t.Fatalf("runSessionLoop() error = %v", err)
	}
	if len(conn.written) == 0 {
		t.Fatal("no payload was written to tunnel")
	}
	requestID, payload, err := decodeReliableRequest(conn.written)
	if err != nil {
		t.Fatalf("decodeReliableRequest(write) error = %v", err)
	}
	if requestID != "req-1" {
		t.Fatalf("requestID = %q, want req-1", requestID)
	}
	if string(payload) != "hello" {
		t.Fatalf("payload = %q, want hello", payload)
	}
	if !conn.closed {
		t.Fatal("connection was not closed")
	}

	responses := decodeSessionResponses(t, output.String())
	if len(responses) != 3 {
		t.Fatalf("responses = %d, want 3: %s", len(responses), output.String())
	}
	if !responses[0].OK || responses[0].Event != "ready" {
		t.Fatalf("first response = %+v, want ready", responses[0])
	}
	if !responses[1].OK || responses[1].Payload != "reply: hello" || responses[1].Attempts != 1 {
		t.Fatalf("send response = %+v, want reply payload", responses[1])
	}
	if !responses[2].OK || responses[2].Event != "closed" {
		t.Fatalf("close response = %+v, want closed", responses[2])
	}
}

func TestRunSessionLoopLogsSendTiming(t *testing.T) {
	response, err := encodeReliableResponse("req-1", []byte("reply: hello"))
	if err != nil {
		t.Fatalf("encodeReliableResponse() error = %v", err)
	}
	conn := &fakePacketConn{
		readData: response,
		readAddr: fakeAddr("target"),
	}
	input := strings.NewReader(`{"op":"send","payload":"hello"}` + "\n")
	var output bytes.Buffer
	var logs bytes.Buffer

	originalWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalWriter)

	err = runSessionLoop(conn, input, &output, ReliabilityConfig{
		MaxRetries:      3,
		Timeout:         time.Millisecond,
		MaxPayloadBytes: 1000,
	}, func() string { return "req-1" })
	if err != nil {
		t.Fatalf("runSessionLoop() error = %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, "session send start request_id=req-1 payload_bytes=5") {
		t.Fatalf("logs missing send start: %s", got)
	}
	if !strings.Contains(got, "session send success request_id=req-1 attempts=1") {
		t.Fatalf("logs missing send success: %s", got)
	}
	if !strings.Contains(got, "duration_ms=") {
		t.Fatalf("logs missing duration: %s", got)
	}
}

func decodeSessionResponses(t *testing.T, data string) []sessionResponse {
	t.Helper()
	var responses []sessionResponse
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		var rsp sessionResponse
		if err := json.Unmarshal(scanner.Bytes(), &rsp); err != nil {
			t.Fatalf("json.Unmarshal(%q) error = %v", scanner.Text(), err)
		}
		responses = append(responses, rsp)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error = %v", err)
	}
	return responses
}
