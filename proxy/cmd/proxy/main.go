package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/quic-go/masque-go"

	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
)

const defaultProxyURL = "https://192.168.1.45:443"

type config struct {
	templateStr string
	bind        string
	keyFile     string
	certFile    string
}

func main() {
	cfg, err := parseConfig(osArgs())
	if err != nil {
		log.Fatal(err)
	}

	template, err := uritemplate.New(cfg.templateStr)
	if err != nil {
		log.Fatalf("invalid template: %v", err)
	}
	cert, err := tls.LoadX509KeyPair(cfg.certFile, cfg.keyFile)
	if err != nil {
		log.Fatalf("failed to load certificate: %v", err)
	}
	tlsConf := http3.ConfigureTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	server := http3.Server{
		Addr:            cfg.bind,
		TLSConfig:       tlsConf,
		EnableDatagrams: true,
		Logger:          slog.Default(),
	}
	defer server.Close()
	proxy := masque.Proxy{}
	// parse the template to extract the path for the HTTP handler
	u, err := url.Parse(cfg.templateStr)
	if err != nil {
		log.Fatalf("failed to parse URI template: %v", err)
	}
	http.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		req, err := masque.ParseProxyRequest(r, template)
		if err != nil {
			var perr *masque.ProxyRequestParseError
			if errors.As(err, &perr) {
				w.WriteHeader(perr.HTTPStatus)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		proxy.Proxy(w, req)
	})
	log.Print(startupLogMessage(cfg))
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("failed to run proxy: %v", err)
	}
}

func osArgs() []string {
	return os.Args[1:]
}

func parseConfig(args []string) (config, error) {
	cfg := config{
		bind:     "0.0.0.0:443",
		keyFile:  "key.pem",
		certFile: "cert.pem",
	}
	proxyURL := defaultProxyURL

	fs := flag.NewFlagSet("proxy", flag.ContinueOnError)
	fs.StringVar(&cfg.templateStr, "t", "", "URI template")
	fs.StringVar(&proxyURL, "proxy", proxyURL, "MASQUE proxy public URL used to build the default URI template")
	fs.StringVar(&cfg.bind, "b", cfg.bind, "bind to (ip:port)")
	fs.StringVar(&cfg.keyFile, "k", cfg.keyFile, "key file")
	fs.StringVar(&cfg.certFile, "c", cfg.certFile, "cert file")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.templateStr == "" {
		cfg.templateStr = defaultURITemplate(proxyURL)
	}
	return cfg, nil
}

func defaultURITemplate(proxyURL string) string {
	return strings.TrimRight(proxyURL, "/") + "/masque?h={target_host}&p={target_port}"
}

func startupLogMessage(cfg config) string {
	return "MASQUE Proxy ready: listen=" + cfg.bind +
		" template=" + cfg.templateStr +
		" cert=" + cfg.certFile +
		" key=" + cfg.keyFile
}
