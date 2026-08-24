package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

type ReliabilityConfig struct {
	TTL             time.Duration
	MaxEntries      int
	MaxPayloadBytes int
	MaxRetries      int
	Timeout         time.Duration
}

type reliabilityConfigFile struct {
	TTL             string `json:"ttl"`
	MaxEntries      int    `json:"max_entries"`
	MaxPayloadBytes int    `json:"max_payload"`
	MaxRetries      *int   `json:"max_retries"`
	Timeout         string `json:"timeout"`
}

type reliableEnvelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Payload   []byte `json:"payload"`
}

func defaultReliabilityConfig() ReliabilityConfig {
	return ReliabilityConfig{
		TTL:             2 * time.Minute,
		MaxEntries:      10000,
		MaxPayloadBytes: 1000,
		MaxRetries:      3,
		Timeout:         time.Second,
	}
}

func loadReliabilityConfig(path string, cfg ReliabilityConfig) (ReliabilityConfig, error) {
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReliabilityConfig{}, fmt.Errorf("read reliability config: %w", err)
	}
	var raw reliabilityConfigFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return ReliabilityConfig{}, fmt.Errorf("parse reliability config: %w", err)
	}
	if raw.TTL != "" {
		ttl, err := time.ParseDuration(raw.TTL)
		if err != nil {
			return ReliabilityConfig{}, fmt.Errorf("invalid ttl: %w", err)
		}
		cfg.TTL = ttl
	}
	if raw.MaxEntries > 0 {
		cfg.MaxEntries = raw.MaxEntries
	}
	if raw.MaxPayloadBytes > 0 {
		cfg.MaxPayloadBytes = raw.MaxPayloadBytes
	}
	if raw.MaxRetries != nil {
		cfg.MaxRetries = *raw.MaxRetries
	}
	if raw.Timeout != "" {
		timeout, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			return ReliabilityConfig{}, fmt.Errorf("invalid timeout: %w", err)
		}
		cfg.Timeout = timeout
	}
	if err := validateReliabilityConfig(cfg); err != nil {
		return ReliabilityConfig{}, err
	}
	return cfg, nil
}

func validateReliabilityConfig(cfg ReliabilityConfig) error {
	if cfg.TTL <= 0 {
		return errors.New("ttl must be positive")
	}
	if cfg.MaxEntries <= 0 {
		return errors.New("max_entries must be positive")
	}
	if cfg.MaxPayloadBytes <= 0 {
		return errors.New("max_payload must be positive")
	}
	if cfg.MaxRetries < 0 {
		return errors.New("max_retries must be zero or positive")
	}
	if cfg.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}

func encodeReliableRequest(requestID string, payload []byte) ([]byte, error) {
	return json.Marshal(reliableEnvelope{
		Type:      "request",
		RequestID: requestID,
		Payload:   payload,
	})
}

func decodeReliableRequest(data []byte) (string, []byte, error) {
	var env reliableEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return "", nil, err
	}
	if env.Type != "request" {
		return "", nil, fmt.Errorf("unexpected envelope type %q", env.Type)
	}
	if env.RequestID == "" {
		return "", nil, errors.New("missing request_id")
	}
	return env.RequestID, append([]byte(nil), env.Payload...), nil
}

func encodeReliableResponse(requestID string, payload []byte) ([]byte, error) {
	return json.Marshal(reliableEnvelope{
		Type:      "response",
		RequestID: requestID,
		Payload:   payload,
	})
}

func decodeReliableResponse(data []byte) (string, []byte, error) {
	var env reliableEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return "", nil, err
	}
	if env.Type != "response" {
		return "", nil, fmt.Errorf("unexpected envelope type %q", env.Type)
	}
	if env.RequestID == "" {
		return "", nil, errors.New("missing request_id")
	}
	return env.RequestID, append([]byte(nil), env.Payload...), nil
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func exchangeReliablePayload(conn net.PacketConn, payload []byte, cfg ReliabilityConfig, requestID string) ([]byte, net.Addr, int, error) {
	if len(payload) > cfg.MaxPayloadBytes {
		return nil, nil, 0, fmt.Errorf("payload size %d exceeds max_payload %d", len(payload), cfg.MaxPayloadBytes)
	}
	request, err := encodeReliableRequest(requestID, payload)
	if err != nil {
		return nil, nil, 0, err
	}
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxRetries+1; attempt++ {
		attemptStarted := time.Now()
		log.Printf("reliable attempt start request_id=%s attempt=%d payload_bytes=%d", requestID, attempt, len(payload))
		if err := sendPayload(conn, request); err != nil {
			return nil, nil, attempt, err
		}
		waitStarted := time.Now()
		response, addr, err := readResponse(conn, cfg.Timeout, reliableReadBufferSize(cfg))
		if err != nil {
			log.Printf(
				"reliable attempt read_error request_id=%s attempt=%d wait_ms=%d total_ms=%d error=%v",
				requestID,
				attempt,
				time.Since(waitStarted).Milliseconds(),
				time.Since(attemptStarted).Milliseconds(),
				err,
			)
			lastErr = err
			continue
		}
		responseID, responsePayload, err := decodeReliableResponse(response)
		if err != nil {
			log.Printf(
				"reliable attempt decode_error request_id=%s attempt=%d wait_ms=%d total_ms=%d error=%v",
				requestID,
				attempt,
				time.Since(waitStarted).Milliseconds(),
				time.Since(attemptStarted).Milliseconds(),
				err,
			)
			lastErr = err
			continue
		}
		if responseID != requestID {
			log.Printf(
				"reliable attempt mismatched_response request_id=%s attempt=%d response_id=%s wait_ms=%d total_ms=%d",
				requestID,
				attempt,
				responseID,
				time.Since(waitStarted).Milliseconds(),
				time.Since(attemptStarted).Milliseconds(),
			)
			lastErr = fmt.Errorf("unexpected response request_id %q", responseID)
			continue
		}
		log.Printf(
			"reliable attempt success request_id=%s attempt=%d wait_ms=%d total_ms=%d response_bytes=%d from=%s",
			requestID,
			attempt,
			time.Since(waitStarted).Milliseconds(),
			time.Since(attemptStarted).Milliseconds(),
			len(responsePayload),
			addr,
		)
		return responsePayload, addr, attempt, nil
	}
	return nil, nil, cfg.MaxRetries + 1, fmt.Errorf("request %s failed after %d retries: %w", requestID, cfg.MaxRetries, lastErr)
}

func reliableReadBufferSize(cfg ReliabilityConfig) int {
	size := cfg.MaxPayloadBytes*2 + 512
	if size < 2048 {
		return 2048
	}
	return size
}
