package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultURITemplate(t *testing.T) {
	tmpl, err := defaultURITemplate("https://192.168.1.10:443")
	if err != nil {
		t.Fatalf("defaultURITemplate() error = %v", err)
	}
	want := "https://192.168.1.10:443/masque?h={target_host}&p={target_port}"
	if tmpl != want {
		t.Fatalf("template = %q, want %q", tmpl, want)
	}
}

func TestParseConfigUsesDefaultProxyAndPayload(t *testing.T) {
	cfg, err := parseConfig([]string{"-target", "192.168.123.100:7400"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.proxyURL != "https://192.168.1.45:443" {
		t.Fatalf("proxyURL = %q", cfg.proxyURL)
	}
	if cfg.uriTemplate != "https://192.168.1.45:443/masque?h={target_host}&p={target_port}" {
		t.Fatalf("uriTemplate = %q", cfg.uriTemplate)
	}
	if cfg.target != "192.168.123.100:7400" {
		t.Fatalf("target = %q", cfg.target)
	}
	if cfg.payload != "hello" {
		t.Fatalf("payload = %q", cfg.payload)
	}
	if cfg.reliability.TTL != 2*time.Minute {
		t.Fatalf("TTL = %s", cfg.reliability.TTL)
	}
	if cfg.reliability.MaxEntries != 10000 {
		t.Fatalf("MaxEntries = %d", cfg.reliability.MaxEntries)
	}
	if cfg.reliability.MaxPayloadBytes != 1000 {
		t.Fatalf("MaxPayloadBytes = %d", cfg.reliability.MaxPayloadBytes)
	}
	if cfg.reliability.MaxRetries != 3 {
		t.Fatalf("MaxRetries = %d", cfg.reliability.MaxRetries)
	}
	if cfg.reliability.Timeout != time.Second {
		t.Fatalf("Timeout = %s", cfg.reliability.Timeout)
	}
}

func TestParseConfigAllowsProxyOverride(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-proxy", "https://192.168.1.20:443",
		"-target", "192.168.123.100:7400",
		"-payload", "ping",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	want := "https://192.168.1.20:443/masque?h={target_host}&p={target_port}"
	if cfg.uriTemplate != want {
		t.Fatalf("uriTemplate = %q, want %q", cfg.uriTemplate, want)
	}
	if cfg.payload != "ping" {
		t.Fatalf("payload = %q", cfg.payload)
	}
}

func TestParseConfigLoadsReliabilityConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reliability.json")
	if err := os.WriteFile(path, []byte(`{
		"ttl": "30s",
		"max_entries": 7,
		"max_payload": 123,
		"max_retries": 5,
		"timeout": "250ms"
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := parseConfig([]string{
		"-config", path,
		"-target", "192.168.123.100:7400",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.reliability.TTL != 30*time.Second {
		t.Fatalf("TTL = %s", cfg.reliability.TTL)
	}
	if cfg.reliability.MaxEntries != 7 {
		t.Fatalf("MaxEntries = %d", cfg.reliability.MaxEntries)
	}
	if cfg.reliability.MaxPayloadBytes != 123 {
		t.Fatalf("MaxPayloadBytes = %d", cfg.reliability.MaxPayloadBytes)
	}
	if cfg.reliability.MaxRetries != 5 {
		t.Fatalf("MaxRetries = %d", cfg.reliability.MaxRetries)
	}
	if cfg.reliability.Timeout != 250*time.Millisecond {
		t.Fatalf("Timeout = %s", cfg.reliability.Timeout)
	}
}

func TestParseConfigRejectsPayloadLargerThanConfiguredLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reliability.json")
	if err := os.WriteFile(path, []byte(`{"max_payload": 3}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := parseConfig([]string{
		"-config", path,
		"-target", "192.168.123.100:7400",
		"-payload", "hello",
	})
	if err == nil {
		t.Fatal("parseConfig() error = nil, want payload size error")
	}
}

func TestParseConfigAllowsTemplateOverride(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-t", "https://proxy.example/udp/{target_host}/{target_port}",
		"-target", "192.168.123.100:7400",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	want := "https://proxy.example/udp/{target_host}/{target_port}"
	if cfg.uriTemplate != want {
		t.Fatalf("uriTemplate = %q, want %q", cfg.uriTemplate, want)
	}
}
