package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

type sessionCommand struct {
	Op      string `json:"op"`
	Payload string `json:"payload,omitempty"`
}

type sessionResponse struct {
	OK       bool   `json:"ok"`
	Event    string `json:"event,omitempty"`
	Payload  string `json:"payload,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
	Error    string `json:"error,omitempty"`
}

func runSession(ctx context.Context, cfg config, in io.Reader, out io.Writer) error {
	tlsConf := &tls.Config{NextProtos: []string{http3.NextProtoH3}}
	if cfg.insecure {
		tlsConf.InsecureSkipVerify = true
	}
	tr := masque.Transport{
		TLSClientConfig: tlsConf,
		QUICConfig: &quic.Config{
			EnableDatagrams:   true,
			InitialPacketSize: 1350,
		},
	}

	dialCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	conn, err := connectTunnel(dialCtx, cfg, func(req *masque.Request) (tunnelConn, *http.Response, error) {
		return tr.Dial(req)
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("connected MASQUE UDP tunnel: %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
	return runSessionLoop(conn, in, out, cfg.reliability, newRequestID)
}

func runSessionLoop(conn net.PacketConn, in io.Reader, out io.Writer, cfg ReliabilityConfig, nextRequestID func() string) error {
	encoder := json.NewEncoder(out)
	if err := encoder.Encode(sessionResponse{OK: true, Event: "ready"}); err != nil {
		return fmt.Errorf("write ready response: %w", err)
	}

	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		var cmd sessionCommand
		if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
			if writeErr := encoder.Encode(sessionResponse{OK: false, Error: fmt.Sprintf("invalid command JSON: %v", err)}); writeErr != nil {
				return fmt.Errorf("write error response: %w", writeErr)
			}
			continue
		}

		switch strings.ToLower(cmd.Op) {
		case "send":
			if err := runSessionSend(conn, encoder, []byte(cmd.Payload), cfg, nextRequestID); err != nil {
				return err
			}
		case "close":
			if err := conn.Close(); err != nil {
				if writeErr := encoder.Encode(sessionResponse{OK: false, Error: err.Error()}); writeErr != nil {
					return fmt.Errorf("write close error response: %w", writeErr)
				}
				return err
			}
			return encoder.Encode(sessionResponse{OK: true, Event: "closed"})
		default:
			if err := encoder.Encode(sessionResponse{OK: false, Error: fmt.Sprintf("unsupported op %q", cmd.Op)}); err != nil {
				return fmt.Errorf("write error response: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read session command: %w", err)
	}
	return nil
}

func runSessionSend(conn net.PacketConn, encoder *json.Encoder, payload []byte, cfg ReliabilityConfig, nextRequestID func() string) error {
	requestID := nextRequestID()
	started := time.Now()
	log.Printf("session send start request_id=%s payload_bytes=%d", requestID, len(payload))
	responsePayload, _, attempts, err := exchangeReliablePayload(conn, payload, cfg, requestID)
	if err != nil {
		log.Printf(
			"session send failure request_id=%s attempts=%d duration_ms=%d error=%v",
			requestID,
			attempts,
			time.Since(started).Milliseconds(),
			err,
		)
		if writeErr := encoder.Encode(sessionResponse{OK: false, Error: err.Error(), Attempts: attempts}); writeErr != nil {
			return fmt.Errorf("write send error response: %w", writeErr)
		}
		return nil
	}
	log.Printf(
		"session send success request_id=%s attempts=%d duration_ms=%d response_bytes=%d",
		requestID,
		attempts,
		time.Since(started).Milliseconds(),
		len(responsePayload),
	)
	return encoder.Encode(sessionResponse{
		OK:       true,
		Payload:  string(responsePayload),
		Attempts: attempts,
	})
}

func commandContext(cfg config) (context.Context, context.CancelFunc) {
	if cfg.session {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), cfg.timeout)
}
