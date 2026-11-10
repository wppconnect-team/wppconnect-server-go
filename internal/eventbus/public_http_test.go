package eventbus

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestPublicWebhookRefusesPrivateAndAmbiguousDNSBeforeDial(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "172.16.2.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "192.0.2.1", "198.18.0.1", "224.0.0.1", "255.255.255.255", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "2001:db8::1", "2002:7f00:1::", "2001:10::1", "3fff::1"} {
		t.Run(address, func(t *testing.T) {
			dialed := false
			dial := publicDialContext(func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP(address)}}, nil
			}, func(context.Context, string, string) (net.Conn, error) {
				dialed = true
				return nil, errors.New("unexpected dial")
			})
			if _, err := dial(t.Context(), "tcp", "receiver.example:443"); err == nil || dialed {
				t.Fatal("private or mixed DNS destination was dialed")
			}
		})
	}
}

func TestPublicWebhookPinsDNSAnswerForConnectionAndRechecksLater(t *testing.T) {
	lookups := 0
	var destinations []string
	dial := publicDialContext(func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups == 1 {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}, func(_ context.Context, network, address string) (net.Conn, error) {
		destinations = append(destinations, address)
		return nil, errors.New("fixture offline")
	})
	_, _ = dial(t.Context(), "tcp", "receiver.example:443")
	_, _ = dial(t.Context(), "tcp", "receiver.example:443")
	if lookups != 2 || len(destinations) != 1 || destinations[0] != "1.1.1.1:443" {
		t.Fatalf("unexpected DNS/dials: %d %v", lookups, destinations)
	}
}

func TestPublicWebhookRequiresHTTPSAndDisablesProxyAndRedirect(t *testing.T) {
	for _, target := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com:8443", "https://example.com/#fragment", "https://"} {
		if _, err := NewPublicHTTP(target); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
	target, err := NewPublicHTTP("https://example.com/receiver")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if target.Client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy bypasses address validation")
	}
	if target.Client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects enabled")
	}
	for _, address := range []string{"1.1.1.1", "8.8.8.8", "2606:4700::1111"} {
		if !publicAddress(net.ParseIP(address)) {
			t.Fatalf("public address rejected: %s", address)
		}
	}
}
