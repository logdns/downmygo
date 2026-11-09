// Package debrid turns a link a premium service hosts into a direct one,
// through Real-Debrid or AllDebrid: the two services that answer with a
// plain HTTP link a downloader can use.
package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider is a service that unrestricts links.
type Provider string

const (
	RealDebrid Provider = "real-debrid"
	AllDebrid  Provider = "alldebrid"
)

// Link is a resolved link.
type Link struct {
	URL      string
	FileName string
	Size     int64
	Host     string
}

// Resolve asks the service for a direct link.
func Resolve(ctx context.Context, p Provider, token, rawURL string) (Link, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Link{}, errors.New("the service needs an API token")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	switch p {
	case RealDebrid:
		return realDebrid(ctx, client, token, rawURL)
	case AllDebrid:
		return allDebrid(ctx, client, token, rawURL)
	}
	return Link{}, fmt.Errorf("unknown service %q", p)
}

func realDebrid(ctx context.Context, client *http.Client, token, rawURL string) (Link, error) {
	endpoint := "https://api.real-debrid.com/rest/1.0/unrestrict/link?auth_token=" +
		url.QueryEscape(token) + "&link=" + url.QueryEscape(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Link{}, err
	}
	var body struct {
		Download string `json:"download"`
		FileName string `json:"filename"`
		Filesize int64  `json:"filesize"`
		Host     string `json:"host"`
		Error    string `json:"error"`
		Code     int    `json:"error_code"`
	}
	if err := getJSON(client, req, &body); err != nil {
		return Link{}, err
	}
	if body.Error != "" {
		return Link{}, fmt.Errorf("real-debrid: %s", body.Error)
	}
	if body.Download == "" {
		return Link{}, errors.New("real-debrid returned no link")
	}
	return Link{URL: body.Download, FileName: body.FileName, Size: body.Filesize, Host: body.Host}, nil
}

func allDebrid(ctx context.Context, client *http.Client, token, rawURL string) (Link, error) {
	endpoint := "https://api.alldebrid.com/v4/link/unlock?agent=downmygo&apikey=" +
		url.QueryEscape(token) + "&link=" + url.QueryEscape(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Link{}, err
	}
	var body struct {
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Data struct {
			Link     string `json:"link"`
			FileName string `json:"filename"`
			Filesize int64  `json:"filesize"`
			Host     string `json:"host"`
		} `json:"data"`
	}
	if err := getJSON(client, req, &body); err != nil {
		return Link{}, err
	}
	if body.Status != "success" {
		msg := body.Error.Message
		if msg == "" {
			msg = body.Error.Code
		}
		if msg == "" {
			msg = "the service refused the link"
		}
		return Link{}, fmt.Errorf("alldebrid: %s", msg)
	}
	if body.Data.Link == "" {
		return Link{}, errors.New("alldebrid returned no link")
	}
	return Link{
		URL: body.Data.Link, FileName: body.Data.FileName,
		Size: body.Data.Filesize, Host: body.Data.Host,
	}, nil
}

func getJSON(client *http.Client, req *http.Request, dst any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("the service answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("the service answered something else: %w", err)
	}
	return nil
}
