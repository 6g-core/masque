package masque

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yosida95/uritemplate/v3"
)

func TestParseProxyRequestExtractsClientIP(t *testing.T) {
	template := uritemplate.MustNew("https://192.168.1.10:443/masque?h={target_host}&p={target_port}")
	req := httptest.NewRequest(http.MethodGet, "https://192.168.1.10:443/masque?h=192.168.123.100&p=7400", nil)
	req.Method = http.MethodConnect
	req.Proto = requestProtocol
	req.RemoteAddr = "192.168.1.20:53001"

	proxyReq, err := ParseProxyRequest(req, template)
	if err != nil {
		t.Fatalf("ParseProxyRequest() error = %v", err)
	}
	if got, want := proxyReq.ClientIP, "192.168.1.20"; got != want {
		t.Fatalf("ClientIP = %q, want %q", got, want)
	}
}

func TestWrapClientIPPayload(t *testing.T) {
	got := wrapClientIPPayload("192.168.1.20", []byte("hello"))
	want := []byte("CLIENT-IP: 192.168.1.20\r\n\r\nhello")
	if string(got) != string(want) {
		t.Fatalf("wrapped payload = %q, want %q", got, want)
	}
}
