package target

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

const defaultReadBufferSize = 2048

type Config struct {
	ListenAddr     string
	ReplyPrefix    string
	ReadBufferSize int
	Logger         *log.Logger
	Reliability    ReliabilityConfig
}

type Server struct {
	cfg Config

	mu    sync.Mutex
	conn  *net.UDPConn
	cache *reliableCache
}

type Message struct {
	ClientIP string
	Payload  []byte
}

func NewServer(cfg Config) *Server {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:7400"
	}
	if cfg.ReadBufferSize <= 0 {
		cfg.ReadBufferSize = defaultReadBufferSize
	}
	cfg.Reliability = normalizeReliabilityConfig(cfg.Reliability)
	return &Server{
		cfg:   cfg,
		cache: newReliableCache(cfg.Reliability),
	}
}

func (s *Server) Serve(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("resolve listen address: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
		_ = conn.Close()
	}()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	s.logf("Target UDP Server listening on %s", conn.LocalAddr())
	buf := make([]byte, s.cfg.ReadBufferSize)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read udp: %w", err)
		}

		msg, _ := parseClientIPPayload(buf[:n])
		payload := append([]byte(nil), msg.Payload...)
		s.logReceivedMessage(remote, msg.ClientIP, payload)
		reply := s.replyPayload(payload)
		if _, err := conn.WriteToUDP(reply, remote); err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("write udp reply to %s: %w", remote, err)
		}
	}
}

func (s *Server) logReceivedMessage(remote net.Addr, clientIP string, payload []byte) {
	requestID, reliablePayload, ok := decodeReliableRequest(payload)
	if ok {
		if clientIP != "" {
			s.logf("received reliable request from %s for client %s request_id=%s payload=%q", remote, clientIP, requestID, reliablePayload)
			return
		}
		s.logf("received reliable request from %s request_id=%s payload=%q", remote, requestID, reliablePayload)
		return
	}
	if clientIP != "" {
		s.logf("received from %s for client %s: %q", remote, clientIP, payload)
		return
	}
	s.logf("received from %s: %q", remote, payload)
}

func (s *Server) replyPayload(payload []byte) []byte {
	requestID, reliablePayload, ok := decodeReliableRequest(payload)
	if !ok {
		return append([]byte(s.cfg.ReplyPrefix), payload...)
	}
	if len(reliablePayload) > s.cfg.Reliability.MaxPayloadBytes {
		response, err := encodeReliableResponse(requestID, []byte("error: payload too large"))
		if err != nil {
			return []byte("error: payload too large")
		}
		return response
	}
	responsePayload, duplicate := s.cache.reply(requestID, func() []byte {
		return append([]byte(s.cfg.ReplyPrefix), reliablePayload...)
	})
	if duplicate {
		s.logf("duplicate reliable request request_id=%s", requestID)
	}
	response, err := encodeReliableResponse(requestID, responsePayload)
	if err != nil {
		return []byte("error: encode reliable response")
	}
	return response
}

func parseClientIPPayload(data []byte) (Message, bool) {
	const prefix = "CLIENT-IP: "
	separator := []byte("\r\n\r\n")
	headerEnd := bytes.Index(data, separator)
	if headerEnd < 0 || !bytes.HasPrefix(data, []byte(prefix)) {
		return Message{Payload: append([]byte(nil), data...)}, false
	}
	clientIP := string(data[len(prefix):headerEnd])
	payload := data[headerEnd+len(separator):]
	return Message{
		ClientIP: clientIP,
		Payload:  append([]byte(nil), payload...),
	}, true
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return ""
	}
	return s.conn.LocalAddr().String()
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *Server) logf(format string, args ...any) {
	if s.cfg.Logger == nil {
		return
	}
	s.cfg.Logger.Printf(format, args...)
}

func NewLogger(w io.Writer) *log.Logger {
	return log.New(w, "", log.LstdFlags)
}
