// Package icy — header parsing for ICY response headers.
package icy

import (
	"net/http"
	"strconv"
	"strings"
)

// Headers holds captured ICY response headers from upstream.
type Headers struct {
	Name     string
	Genre    string
	Bitrate  int
	Metaint  int
	URL      string
	Pub      string
	All      map[string]string // all icy-* headers, lowercased keys
}

// ParseHeaders extracts ICY headers from an HTTP response.
// Icecast sends these as standard HTTP headers with icy- prefix.
func ParseHeaders(resp *http.Response) *Headers {
	h := &Headers{
		All: make(map[string]string),
	}

	for key, vals := range resp.Header {
		lower := strings.ToLower(key)
		if !strings.HasPrefix(lower, "icy-") {
			continue
		}
		val := ""
		if len(vals) > 0 {
			val = vals[0]
		}
		h.All[lower] = val

		switch lower {
		case "icy-name":
			h.Name = val
		case "icy-genre":
			h.Genre = val
		case "icy-br":
			h.Bitrate, _ = strconv.Atoi(val)
		case "icy-metaint":
			h.Metaint, _ = strconv.Atoi(val)
		case "icy-url":
			h.URL = val
		case "icy-pub":
			h.Pub = val
		}
	}

	return h
}

// ForwardableHeaders returns the ICY headers that should be sent to the
// client — everything except icy-metaint (we strip metadata, so the
// client must not expect it).
func (h *Headers) ForwardableHeaders() map[string]string {
	fwd := make(map[string]string, len(h.All))
	for k, v := range h.All {
		if k == "icy-metaint" {
			continue
		}
		fwd[k] = v
	}
	return fwd
}
