package eventbus

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// NewPublicHTTP is for tenant-supplied webhook destinations. DNS is resolved and
// checked on every new connection; the checked address is dialed directly so a
// second lookup cannot redirect the request into the service's private network.
func NewPublicHTTP(target string) (*HTTP, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, errors.New("public webhook must use HTTPS on port 443 without credentials or fragment")
	}
	h, err := NewHTTP(target)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	h.Client.Transport = &http.Transport{
		Proxy:             nil,
		DialContext:       publicDialContext(net.DefaultResolver.LookupIPAddr, dialer.DialContext),
		ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 2,
		IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
	}
	return h, nil
}

var nonPublicV4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func publicAddress(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is4() {
		for _, prefix := range nonPublicV4 {
			if prefix.Contains(address) {
				return false
			}
		}
		return true
	}
	return netip.MustParsePrefix("2000::/3").Contains(address) &&
		!netip.MustParsePrefix("2001:db8::/32").Contains(address) &&
		!netip.MustParsePrefix("2002::/16").Contains(address) &&
		!netip.MustParsePrefix("2001::/23").Contains(address) &&
		!netip.MustParsePrefix("3fff::/20").Contains(address)
}

func publicDialContext(resolve func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("public webhook port is not allowed")
		}
		ips, err := resolve(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("public webhook hostname could not be resolved")
		}
		for _, ip := range ips {
			if ip.Zone != "" || !publicAddress(ip.IP) {
				return nil, errors.New("webhook destination is not public")
			}
		}
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("public webhook connection failed")
	}
}
