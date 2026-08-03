package main

import "testing"

func TestParseConfigUsesRunnableDefaults(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig(nil) error = %v", err)
	}

	if cfg.bind != "0.0.0.0:443" {
		t.Fatalf("bind = %q, want 0.0.0.0:443", cfg.bind)
	}
	if cfg.certFile != "cert.pem" {
		t.Fatalf("certFile = %q, want cert.pem", cfg.certFile)
	}
	if cfg.keyFile != "key.pem" {
		t.Fatalf("keyFile = %q, want key.pem", cfg.keyFile)
	}
	if cfg.templateStr != "https://192.168.1.45:443/masque?h={target_host}&p={target_port}" {
		t.Fatalf("templateStr = %q", cfg.templateStr)
	}
}

func TestParseConfigAllowsProxyURLOverride(t *testing.T) {
	cfg, err := parseConfig([]string{"-proxy", "https://192.168.1.20:443"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	if cfg.templateStr != "https://192.168.1.20:443/masque?h={target_host}&p={target_port}" {
		t.Fatalf("templateStr = %q", cfg.templateStr)
	}
}

func TestStartupLogMessageShowsProxyIsReadyToListen(t *testing.T) {
	msg := startupLogMessage(config{
		bind:        "0.0.0.0:443",
		templateStr: "https://192.168.1.45:443/masque?h={target_host}&p={target_port}",
		certFile:    "cert.pem",
		keyFile:     "key.pem",
	})

	want := "MASQUE Proxy ready: listen=0.0.0.0:443 template=https://192.168.1.45:443/masque?h={target_host}&p={target_port} cert=cert.pem key=key.pem"
	if msg != want {
		t.Fatalf("startupLogMessage() = %q, want %q", msg, want)
	}
}
