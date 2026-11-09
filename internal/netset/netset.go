// Package netset builds the dialers the downloads use: a local address to
// bind (a VPN's, so a dropped tunnel stops the traffic) and a resolver
// that can be the system's, DNS over HTTPS or DNS over TLS.
package netset

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Spec names a resolver:
//
//	""            the system's
//	"system"      the system's
//	"doh"         DNS over HTTPS, Cloudflare
//	"doh:cloudflare", "doh:google", "doh:quad9", "doh:adguard"
//	"doh:https://host/path"   any RFC 8484 endpoint
//	"dot"         DNS over TLS, Cloudflare
//	"dot:1.1.1.1:853", "dot:9.9.9.9:853"   any DNS over TLS server
type Spec string

// Well-known endpoints.
var dohEndpoints = map[string]string{
	"cloudflare": "https://cloudflare-dns.com/dns-query",
	"google":     "https://dns.google/dns-query",
	"quad9":      "https://dns.quad9.net/dns-query",
	"adguard":    "https://dns.adguard-dns.com/dns-query",
	"mullvad":    "https://dns.mullvad.net/dns-query",
}

var dotServers = map[string]string{
	"cloudflare": "1.1.1.1:853",
	"google":     "8.8.8.8:853",
	"quad9":      "9.9.9.9:853",
	"adguard":    "94.140.14.14:853",
	"mullvad":    "194.242.2.2:853",
}

// Resolver is what a dialer asks for addresses.
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

// NewResolver builds the resolver a spec names, or nil for the system's.
func NewResolver(spec string) (Resolver, error) {
	s := strings.TrimSpace(spec)
	switch {
	case s == "", strings.EqualFold(s, "system"), strings.EqualFold(s, "none"):
		return nil, nil
	case strings.HasPrefix(s, "doh:"):
		return newDoH(s[len("doh:"):])
	case strings.EqualFold(s, "doh"):
		return newDoH("cloudflare")
	case strings.HasPrefix(s, "dot:"):
		return newDoT(s[len("dot:"):])
	case strings.EqualFold(s, "dot"):
		return newDoT("cloudflare")
	}
	return nil, fmt.Errorf("unknown DNS setting %q", spec)
}

// Dialer returns a dialer that resolves with r and binds to bindIP, both
// of which may be empty.
func Dialer(bindIP string, r Resolver) *net.Dialer {
	d := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	if ip := net.ParseIP(strings.TrimSpace(bindIP)); ip != nil {
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	return d
}

// DialContext connects to addr, resolving the host with r when there is
// one. It is the DialContext a transport wants.
func DialContext(d *net.Dialer, r Resolver) func(context.Context, string, string) (net.Conn, error) {
	if r == nil {
		return d.DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return d.DialContext(ctx, network, addr)
		}
		if ip := net.ParseIP(host); ip != nil {
			return d.DialContext(ctx, network, addr)
		}
		ips, err := r.LookupIP(ctx, host)
		if err != nil || len(ips) == 0 {
			if err == nil {
				err = fmt.Errorf("no address for %s", host)
			}
			return nil, err
		}
		var last error
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
}

// --- DNS over HTTPS ---

type doh struct {
	endpoint string
	client   *http.Client
}

func newDoH(spec string) (Resolver, error) {
	endpoint := spec
	if name, ok := dohEndpoints[strings.ToLower(spec)]; ok {
		endpoint = name
	} else if !strings.HasPrefix(spec, "https://") {
		return nil, fmt.Errorf("DNS over HTTPS needs an https:// address, not %q", spec)
	}
	tr := &http.Transport{
		// Resolving the resolver's own name goes through the system, as
		// there is nothing else to ask yet.
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &doh{endpoint: endpoint, client: &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}}, nil
}

func (d *doh) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	q, err := query(host, dnsmessage.TypeA)
	if err != nil {
		return nil, err
	}
	ips, err := d.ask(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		q6, err := query(host, dnsmessage.TypeAAAA)
		if err != nil {
			return nil, err
		}
		return d.ask(ctx, q6)
	}
	return ips, nil
}

func (d *doh) ask(ctx context.Context, msg []byte) ([]net.IP, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, strings.NewReader(string(msg)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DNS over HTTPS answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	return parseAnswers(body, 0)
}

// --- DNS over TLS ---

type dot struct {
	server string
	// tls is the client's configuration; nil is the system's roots.
	tls *tls.Config
}

func newDoT(spec string) (Resolver, error) {
	server := spec
	if name, ok := dotServers[strings.ToLower(spec)]; ok {
		server = name
	}
	if _, _, err := net.SplitHostPort(server); err != nil {
		return nil, fmt.Errorf("DNS over TLS needs host:port, not %q", spec)
	}
	return &dot{server: server}, nil
}

func (d *dot) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	for _, t := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		msg, err := query(host, t)
		if err != nil {
			return nil, err
		}
		ips, err := d.ask(ctx, msg)
		if err != nil {
			return nil, err
		}
		if len(ips) > 0 {
			return ips, nil
		}
	}
	return nil, fmt.Errorf("no address for %s", host)
}

func (d *dot) ask(ctx context.Context, msg []byte) ([]net.IP, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", d.server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	host, _, _ := net.SplitHostPort(d.server)
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if d.tls != nil {
		cfg = d.tls.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = host
		}
	}
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = tc.SetDeadline(deadline)
	} else {
		_ = tc.SetDeadline(time.Now().Add(10 * time.Second))
	}
	// DNS over TCP frames a message with its length.
	frame := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(frame, uint16(len(msg)))
	copy(frame[2:], msg)
	if _, err := tc.Write(frame); err != nil {
		return nil, err
	}
	var n uint16
	if err := binary.Read(tc, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(tc, body); err != nil {
		return nil, err
	}
	return parseAnswers(body, 0)
}

// --- the wire format ---

func query(host string, t dnsmessage.Type) ([]byte, error) {
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		RecursionDesired: true,
		ID:               uint16(time.Now().UnixNano()),
	})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: name, Type: t, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	return b.Finish()
}

func parseAnswers(msg []byte, _ int) ([]net.IP, error) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		return nil, err
	}
	if h.RCode != dnsmessage.RCodeSuccess {
		return nil, fmt.Errorf("DNS answered %s", h.RCode)
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil, err
	}
	var ips []net.IP
	for {
		r, err := p.Answer()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch b := r.Body.(type) {
		case *dnsmessage.AResource:
			ips = append(ips, net.IP(b.A[:]))
		case *dnsmessage.AAAAResource:
			ips = append(ips, net.IP(b.AAAA[:]))
		}
	}
	return ips, nil
}

// Base64 is here for endpoints that take the query in the URL, which some
// servers prefer; the code above posts instead.
func Base64(msg []byte) string { return base64.RawURLEncoding.EncodeToString(msg) }
