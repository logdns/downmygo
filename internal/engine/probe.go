package engine

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/downmygo/downmygo/internal/netset"
)

// RequestOptions are the per-download request settings.
type RequestOptions struct {
	UserAgent string
	Referer   string
	Cookies   string
	Proxy     string // "", "system", or a proxy URL
	BindIP    string // local address to bind, for a VPN kill switch
	DNS       string // "", "system", "doh", "dot", or an endpoint
	MaxConns  int
}

// NewClient builds the HTTP client a download uses. Compression is off so
// that the bytes on the wire are the bytes of the file, which is what a
// range request counts.
func NewClient(o RequestOptions) *http.Client {
	dialer := netset.Dialer(o.BindIP, nil)
	// A resolver that is not the system's resolves every connection, so
	// the names a download uses never go out in clear text.
	resolver, err := netset.NewResolver(o.DNS)
	if err != nil {
		resolver = nil
	}
	tr := &http.Transport{
		Proxy:                 proxyFunc(o.Proxy),
		DialContext:           netset.DialContext(dialer, resolver),
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   max(8, o.MaxConns),
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func proxyFunc(v string) func(*http.Request) (*url.URL, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "direct":
		return nil
	case "system":
		return http.ProxyFromEnvironment
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return nil
	}
	return http.ProxyURL(u)
}

// Info is what a server says about a file before it is downloaded.
type Info struct {
	URL          string
	FinalURL     string
	FileName     string
	Size         int64
	Resumable    bool
	ETag         string
	LastModified string
	ContentType  string
}

// Probe asks a server for the file's size, name and whether it accepts
// range requests, by requesting the first byte.
func Probe(ctx context.Context, client *http.Client, rawURL string, o RequestOptions) (*Info, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("no address")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("bad address: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s links are not supported yet", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req, o)
	req.Header.Set("Range", "bytes=0-0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, friendlyNetError(err)
	}
	defer resp.Body.Close()

	info := &Info{
		URL:          rawURL,
		FinalURL:     resp.Request.URL.String(),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentType:  resp.Header.Get("Content-Type"),
		FileName:     FileNameFromHeaders(resp.Header.Get("Content-Disposition"), resp.Request.URL.String()),
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		info.Resumable = true
		info.Size = sizeFromContentRange(resp.Header.Get("Content-Range"))
		if info.Size <= 0 {
			info.Size = resp.ContentLength
		}
	case http.StatusOK:
		// The server ignored the range: one stream, no resume.
		info.Resumable = false
		info.Size = resp.ContentLength
	case http.StatusRequestedRangeNotSatisfiable:
		// An empty file, or a server that dislikes a 1-byte range.
		info.Size = sizeFromContentRange(resp.Header.Get("Content-Range"))
		info.Resumable = info.Size > 0
		if info.Size == 0 {
			info.Size = 0
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("the server refused access (%s); it may need a login", resp.Status)
	case http.StatusNotFound:
		return nil, errors.New("the server says the file does not exist (404)")
	case http.StatusTooManyRequests:
		return nil, errors.New("the server is busy (429); try again later")
	}
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return nil, fmt.Errorf("the server answered %s", resp.Status)
	}
	if info.Size <= 0 {
		// Unknown length: a single stream is the only honest plan.
		info.Resumable = false
	}
	return info, nil
}

func sizeFromContentRange(v string) int64 {
	// "bytes 0-0/12345" or "bytes */12345"
	if v == "" {
		return 0
	}
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		if n, err := strconv.ParseInt(strings.TrimSpace(v[i+1:]), 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func setHeaders(req *http.Request, o RequestOptions) {
	ua := o.UserAgent
	if ua == "" {
		ua = "downmygo/1.0"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	if o.Referer != "" {
		req.Header.Set("Referer", o.Referer)
	}
	if o.Cookies != "" {
		req.Header.Set("Cookie", o.Cookies)
	}
}

func friendlyNetError(err error) error {
	if err == nil {
		return nil
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Errorf("cannot find %s; check the address and your connection", dnsErr.Name)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "certificate"):
		return fmt.Errorf("the server's certificate could not be verified: %w", err)
	case strings.Contains(msg, "connection refused"):
		return errors.New("the server refused the connection")
	case strings.Contains(msg, "timeout"):
		return errors.New("the server took too long to answer")
	case strings.Contains(msg, "no such host"):
		return errors.New("cannot find that server")
	}
	return err
}

// Drain reads and discards a little of a body so a connection can be
// reused, then closes it.
func Drain(r io.ReadCloser) {
	_, _ = io.CopyN(io.Discard, r, 4<<10)
	_ = r.Close()
}
