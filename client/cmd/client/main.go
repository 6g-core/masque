package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
)

const defaultProxyURL = "https://192.168.1.10:443"

// config 保存单次 Client 启动所需的全部运行参数。
type config struct {
	proxyURL       string
	uriTemplate    string
	target         string
	payload        string
	timeout        time.Duration
	readBufferSize int
	insecure       bool
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

// parseConfig 只负责解析命令行参数，不参与网络流程。
// 这样后续测试或交互式模式可以复用隧道相关函数。
func parseConfig(args []string) (config, error) {
	cfg := config{
		proxyURL:       defaultProxyURL,
		payload:        "hello",
		timeout:        5 * time.Second,
		readBufferSize: 2048,
	}

	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	fs.StringVar(&cfg.proxyURL, "proxy", cfg.proxyURL, "MASQUE proxy URL")
	fs.StringVar(&cfg.uriTemplate, "t", "", "URI template; overrides -proxy derived template")
	fs.StringVar(&cfg.target, "target", "", "target UDP server address, host:port")
	fs.StringVar(&cfg.payload, "payload", cfg.payload, "UDP payload to send")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "dial and read timeout")
	fs.IntVar(&cfg.readBufferSize, "buf", cfg.readBufferSize, "UDP response read buffer size")
	fs.BoolVar(&cfg.insecure, "insecure", false, "skip TLS certificate verification")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.target == "" {
		return config{}, errors.New("missing required -target <host:port>")
	}
	if _, _, err := net.SplitHostPort(cfg.target); err != nil {
		return config{}, fmt.Errorf("invalid -target %q: %w", cfg.target, err)
	}
	if cfg.uriTemplate == "" {
		tmpl, err := defaultURITemplate(cfg.proxyURL)
		if err != nil {
			return config{}, err
		}
		cfg.uriTemplate = tmpl
	}
	if cfg.readBufferSize <= 0 {
		return config{}, errors.New("-buf must be positive")
	}
	if cfg.timeout <= 0 {
		return config{}, errors.New("-timeout must be positive")
	}
	return cfg, nil
}

// defaultURITemplate 根据 Proxy 基础地址生成当前约定的 CONNECT-UDP URI Template。
// 生成结果形如：/masque?h={target_host}&p={target_port}。
func defaultURITemplate(proxyURL string) (string, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return "", fmt.Errorf("invalid -proxy URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("invalid -proxy URL %q: scheme must be https", proxyURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid -proxy URL %q: missing host", proxyURL)
	}
	base := strings.TrimRight(u.Scheme+"://"+u.Host, "/")
	return base + "/masque?h={target_host}&p={target_port}", nil
}

// run 只负责单次命令执行的整体编排：
// 建立隧道、发送一次 payload、读取一次响应，然后返回。
func run(ctx context.Context, cfg config) error {
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

	conn, err := connectTunnel(ctx, cfg, func(req *masque.Request) (tunnelConn, *http.Response, error) {
		return tr.Dial(req)
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("connected MASQUE UDP tunnel: %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
	if err := sendPayload(conn, []byte(cfg.payload)); err != nil {
		return err
	}
	log.Printf("sent payload to target %s: %q", cfg.target, cfg.payload)

	payload, addr, err := readResponse(conn, cfg.timeout, cfg.readBufferSize)
	if err != nil {
		return err
	}
	log.Printf("received response from %s: %s", addr, string(payload))
	return nil
}

// tunnelConn 是当前命令对 masque.Conn 的最小依赖面。
// net.PacketConn 负责 payload 读写，RemoteAddr 只用于日志输出。
type tunnelConn interface {
	net.PacketConn
	RemoteAddr() net.Addr
}

type tunnelDialFunc func(*masque.Request) (tunnelConn, *http.Response, error)

// connectTunnel 只负责 CONNECT-UDP 建连阶段。
// 它不发送业务 payload，调用方可以自行决定何时发送、发送几次。
func connectTunnel(ctx context.Context, cfg config, dial tunnelDialFunc) (tunnelConn, error) {
	req, err := masque.NewRequest(ctx, uritemplate.MustNew(cfg.uriTemplate), cfg.target)
	if err != nil {
		return nil, fmt.Errorf("create CONNECT-UDP request: %w", err)
	}
	conn, rsp, err := dial(req)
	if err != nil {
		return nil, fmt.Errorf("dial MASQUE proxy: %w", err)
	}
	if rsp != nil && (rsp.StatusCode < 200 || rsp.StatusCode >= 300) {
		err := fmt.Errorf("CONNECT-UDP failed: HTTP status %d", rsp.StatusCode)
		if conn != nil {
			return nil, errors.Join(err, conn.Close())
		}
		return nil, err
	}
	return conn, nil
}

// sendPayload 将原始业务 payload 写入已经建立好的 MASQUE UDP 隧道。
// Target 地址已经由 CONNECT-UDP 绑定，因此 WriteTo 的 addr 参数传 nil。
func sendPayload(conn net.PacketConn, payload []byte) error {
	if _, err := conn.WriteTo(payload, nil); err != nil {
		return fmt.Errorf("send payload: %w", err)
	}
	return nil
}

// readResponse 从隧道中读取一个响应 datagram。
// 返回前会复制临时缓冲区中的数据，避免调用方持有可被复用的 buffer。
func readResponse(conn net.PacketConn, timeout time.Duration, readBufferSize int) ([]byte, net.Addr, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, nil, fmt.Errorf("set read deadline: %w", err)
	}
	buf := make([]byte, readBufferSize)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	return append([]byte(nil), buf[:n]...), addr, nil
}
