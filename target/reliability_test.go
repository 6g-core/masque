package target

import (
	"testing"
	"time"
)

func TestReliableRequestRoundTrip(t *testing.T) {
	encoded, err := encodeReliableRequest("req-1", []byte("hello"))
	if err != nil {
		t.Fatalf("encodeReliableRequest() error = %v", err)
	}
	requestID, payload, ok := decodeReliableRequest(encoded)
	if !ok {
		t.Fatal("decodeReliableRequest() did not recognize reliable request")
	}
	if requestID != "req-1" {
		t.Fatalf("requestID = %q", requestID)
	}
	if string(payload) != "hello" {
		t.Fatalf("payload = %q", payload)
	}

	response, err := encodeReliableResponse(requestID, []byte("reply: hello"))
	if err != nil {
		t.Fatalf("encodeReliableResponse() error = %v", err)
	}
	responseID, responsePayload, ok := decodeReliableResponse(response)
	if !ok {
		t.Fatal("decodeReliableResponse() did not recognize reliable response")
	}
	if responseID != "req-1" {
		t.Fatalf("responseID = %q", responseID)
	}
	if string(responsePayload) != "reply: hello" {
		t.Fatalf("responsePayload = %q", responsePayload)
	}
}

func TestReliableCacheReturnsCachedResponseForDuplicateRequest(t *testing.T) {
	cache := newReliableCache(ReliabilityConfig{
		TTL:        time.Minute,
		MaxEntries: 10,
	})

	first, duplicate := cache.reply("req-1", func() []byte {
		return []byte("reply: hello")
	})
	second, duplicateSecond := cache.reply("req-1", func() []byte {
		return []byte("reply: changed")
	})

	if duplicate {
		t.Fatal("first request should not be marked duplicate")
	}
	if !duplicateSecond {
		t.Fatal("second request should be marked duplicate")
	}
	if string(first) != "reply: hello" {
		t.Fatalf("first = %q", first)
	}
	if string(second) != "reply: hello" {
		t.Fatalf("second = %q, want cached response", second)
	}
}

func TestReliableCacheEvictsExpiredEntries(t *testing.T) {
	cache := newReliableCache(ReliabilityConfig{
		TTL:        time.Nanosecond,
		MaxEntries: 10,
	})

	cache.reply("req-1", func() []byte {
		return []byte("reply: hello")
	})
	time.Sleep(time.Millisecond)
	second, duplicate := cache.reply("req-1", func() []byte {
		return []byte("reply: changed")
	})

	if duplicate {
		t.Fatal("expired request should not be marked duplicate")
	}
	if string(second) != "reply: changed" {
		t.Fatalf("second = %q", second)
	}
}
