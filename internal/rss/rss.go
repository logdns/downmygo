// Package rss reads RSS 2.0 and Atom feeds, and says which of their items
// a subscription wants: an enclosure, or a link whose name matches.
package rss

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Item is one entry of a feed.
type Item struct {
	// ID is what tells items apart between fetches: the guid, else the
	// link, else the title and date.
	ID        string
	Title     string
	Link      string
	Enclosure string
	Size      int64
	Published time.Time
}

// Feed is a parsed feed.
type Feed struct {
	Title string
	Items []Item
}

// Parse reads an RSS 2.0 or Atom document.
func Parse(r io.Reader) (*Feed, error) {
	body, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return nil, err
	}
	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("this is not a feed: %w", err)
	}
	switch strings.ToLower(probe.XMLName.Local) {
	case "rss":
		return parseRSS(body)
	case "feed":
		return parseAtom(body)
	}
	return nil, fmt.Errorf("this is not a feed (%s)", probe.XMLName.Local)
}

type rssDoc struct {
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			GUID    string `xml:"guid"`
			PubDate string `xml:"pubDate"`
			Date    string `xml:"date"`
			Encl    []struct {
				URL    string `xml:"url,attr"`
				Length int64  `xml:"length,attr"`
				Type   string `xml:"type,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

func parseRSS(body []byte) (*Feed, error) {
	var doc rssDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	f := &Feed{Title: strings.TrimSpace(doc.Channel.Title)}
	for _, it := range doc.Channel.Items {
		item := Item{
			Title: strings.TrimSpace(it.Title),
			Link:  strings.TrimSpace(it.Link),
			ID:    strings.TrimSpace(it.GUID),
		}
		if item.ID == "" {
			item.ID = item.Link
		}
		for _, e := range it.Encl {
			if e.URL != "" {
				item.Enclosure = strings.TrimSpace(e.URL)
				item.Size = e.Length
				break
			}
		}
		item.Published = parseTime(firstNonEmpty(it.PubDate, it.Date))
		if item.ID == "" {
			item.ID = item.Title + "@" + item.Published.Format(time.RFC3339)
		}
		f.Items = append(f.Items, item)
	}
	return f, nil
}

type atomDoc struct {
	Title string `xml:"title"`
	Entry []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
			Len  int64  `xml:"length,attr"`
		} `xml:"link"`
		Updated string `xml:"updated"`
		Pub     string `xml:"published"`
	} `xml:"entry"`
}

func parseAtom(body []byte) (*Feed, error) {
	var doc atomDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	f := &Feed{Title: strings.TrimSpace(doc.Title)}
	for _, e := range doc.Entry {
		item := Item{
			Title: strings.TrimSpace(e.Title),
			ID:    strings.TrimSpace(e.ID),
		}
		for _, l := range e.Links {
			switch l.Rel {
			case "enclosure":
				if item.Enclosure == "" {
					item.Enclosure = l.Href
					item.Size = l.Len
				}
			case "", "alternate":
				if item.Link == "" {
					item.Link = l.Href
				}
			}
		}
		item.Published = parseTime(firstNonEmpty(e.Pub, e.Updated))
		if item.ID == "" {
			item.ID = firstNonEmpty(item.Link, item.Title)
		}
		f.Items = append(f.Items, item)
	}
	return f, nil
}

// Fetch downloads and parses a feed.
func Fetch(ctx context.Context, client *http.Client, rawURL, userAgent string) (*Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the feed answered %s", resp.Status)
	}
	f, err := Parse(resp.Body)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(f.Items, func(i, j int) bool {
		return f.Items[i].Published.After(f.Items[j].Published)
	})
	return f, nil
}

// Want says whether a subscription takes an item: an item with an
// enclosure always does, else a link whose name ends in one of the
// extensions.
func Want(item Item, extensions []string) (string, bool) {
	if item.Enclosure != "" {
		return item.Enclosure, true
	}
	if item.Link == "" {
		return "", false
	}
	if len(extensions) == 0 {
		return item.Link, true
	}
	name := strings.ToLower(item.Link)
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	for _, e := range extensions {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" && strings.HasSuffix(name, "."+e) {
			return item.Link, true
		}
	}
	return "", false
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
		time.RFC3339Nano, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z0700", "2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
