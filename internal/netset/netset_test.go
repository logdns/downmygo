package netset

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestSpecs(t *testing.T) {
	cases := []struct {
		spec string
		ok   bool
	}{
		{"", true}, {"system", true}, {"doh", true}, {"dot", true},
		{"doh:cloudflare", true}, {"doh:https://dns.example/dns-query", true},
		{"dot:1.1.1.1:853", true},
		{"doh:http://insecure/dns-query", false},
		{"dot:1.1.1.1", false},
		{"dnscrypt", false},
	}
	for _, c := range cases {
		r, err := NewResolver(c.spec)
		if c.ok && err != nil {
			t.Errorf("%q: %v", c.spec, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%q: accepted", c.spec)
		}
		if c.spec == "" && r != nil {
			t.Errorf("the empty spec is not the system's")
		}
	}
}

// answer builds a DNS message with one A record.
func answer(t *testing.T, host string, ip net.IP) []byte {
	t.Helper()
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		t.Fatal(err)
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, RCode: dnsmessage.RCodeSuccess})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	var a [4]byte
	copy(a[:], ip.To4())
	if err := b.AResource(dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: a}); err != nil {
		t.Fatal(err)
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestDoHLookup(t *testing.T) {
	msg := answer(t, "example.com", net.IPv4(93, 184, 216, 34))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/dns-message" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		var p dnsmessage.Parser
		if _, err := p.Start(body); err != nil {
			t.Errorf("the query is not DNS: %v", err)
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(msg)
	}))
	defer srv.Close()

	// The test server's certificate is its own; trust it.
	r := &doh{endpoint: srv.URL, client: srv.Client()}
	ips, err := r.LookupIP(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.IPv4(93, 184, 216, 34)) {
		t.Fatalf("ips = %v", ips)
	}
}

func TestDoTLookup(t *testing.T) {
	msg := answer(t, "example.com", net.IPv4(1, 2, 3, 4))
	cert, err := selfSigned()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var n uint16
				if err := binary.Read(c, binary.BigEndian, &n); err != nil {
					return
				}
				if _, err := io.CopyN(io.Discard, c, int64(n)); err != nil {
					return
				}
				frame := make([]byte, 2+len(msg))
				binary.BigEndian.PutUint16(frame, uint16(len(msg)))
				copy(frame[2:], msg)
				_, _ = c.Write(frame)
			}(conn)
		}
	}()

	r := &dot{server: ln.Addr().String(), tls: &tls.Config{InsecureSkipVerify: true}}
	ips, err := r.LookupIP(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].String() != "1.2.3.4" {
		t.Fatalf("ips = %v", ips)
	}
}

// selfSigned makes a certificate the test's TLS server can use.
func selfSigned() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "downmygo.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"downmygo.test"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func TestParseAnswers(t *testing.T) {
	msg := answer(t, "example.com", net.IPv4(127, 0, 0, 1))
	ips, err := parseAnswers(msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].String() != "127.0.0.1" {
		t.Fatalf("ips = %v", ips)
	}
	if _, err := parseAnswers([]byte("not dns"), 0); err == nil {
		t.Fatal("garbage parsed")
	}
}

func TestDialerBindsAndResolves(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	// A resolver that answers every name with the loopback address.
	r := resolverFunc(func(ctx context.Context, host string) ([]net.IP, error) {
		if host != "downmygo.test" {
			t.Errorf("asked for %q", host)
		}
		return []net.IP{net.IPv4(127, 0, 0, 1)}, nil
	})
	d := Dialer("", r)
	conn, err := DialContext(d, r)(context.Background(), "tcp", "downmygo.test:"+port)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	// A bound dialer only connects from the address it was given.
	d = Dialer("127.0.0.1", nil)
	conn, err = d.DialContext(context.Background(), "tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if d.LocalAddr == nil || !strings.HasPrefix(d.LocalAddr.String(), "127.0.0.1") {
		t.Fatalf("local address = %v", d.LocalAddr)
	}
}

type resolverFunc func(context.Context, string) ([]net.IP, error)

func (f resolverFunc) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return f(ctx, host)
}
