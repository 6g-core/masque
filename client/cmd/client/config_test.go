package main

import "testing"

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
	if cfg.proxyURL != "https://192.168.1.10:443" {
		t.Fatalf("proxyURL = %q", cfg.proxyURL)
	}
	if cfg.uriTemplate != "https://192.168.1.10:443/masque?h={target_host}&p={target_port}" {
		t.Fatalf("uriTemplate = %q", cfg.uriTemplate)
	}
	if cfg.target != "192.168.123.100:7400" {
		t.Fatalf("target = %q", cfg.target)
	}
	if cfg.payload != "hello" {
		t.Fatalf("payload = %q", cfg.payload)
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
