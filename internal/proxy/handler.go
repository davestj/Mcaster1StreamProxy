// Package proxy implements the HTTP stream proxy handler.
// Each request spawns a goroutine that connects upstream, strips ICY metadata,
// pre-buffers, then streams clean audio to the client over HTTPS.
package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mcaster1/Mcaster1StreamProxy/internal/config"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/db"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/icy"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/stats"
)

// Handler serves streaming proxy requests.
type Handler struct {
	cfg       *config.Config
	pool      *db.Pool
	semaphore chan struct{} // limits concurrent streams
	active    atomic.Int64

	// Custom HTTP client for upstream connections — accepts self-signed certs
	client *http.Client
}

// New creates a proxy handler.
func New(cfg *config.Config, pool *db.Pool) *Handler {
	h := &Handler{
		cfg:       cfg,
		pool:      pool,
		semaphore: make(chan struct{}, cfg.Server.MaxConcurrentStreams),
	}

	// HTTP client with relaxed TLS (upstream servers often have self-signed certs)
	h.client = &http.Client{
		Timeout: 0, // no overall timeout — streams are infinite
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 10,
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(cfg.Proxy.ConnectTimeoutSec) * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		// Follow redirects up to max
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= cfg.Proxy.MaxRedirects {
				return fmt.Errorf("too many redirects (%d)", len(via))
			}
			// Re-add ICY header on redirect
			req.Header.Set("Icy-MetaData", "1")
			return nil
		},
	}

	return h
}

// ActiveStreams returns the count of currently active proxy streams.
func (h *Handler) ActiveStreams() int64 {
	return h.active.Load()
}

// ServeStream handles GET /stream?id=N[&buf=small|medium|large]
func (h *Handler) ServeStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse station ID
	idStr := r.URL.Query().Get("id")
	stationID, err := strconv.Atoi(idStr)
	if err != nil || stationID <= 0 {
		http.Error(w, "Missing or invalid station id", http.StatusBadRequest)
		return
	}

	// Acquire concurrency slot
	select {
	case h.semaphore <- struct{}{}:
		defer func() { <-h.semaphore }()
	default:
		http.Error(w, "Too many concurrent streams", http.StatusServiceUnavailable)
		return
	}

	h.active.Add(1)
	defer h.active.Add(-1)

	// Look up station
	station, err := h.pool.LookupStation(stationID)
	if err != nil {
		log.Printf("[ERROR] DB lookup station %d: %v", stationID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if station == nil {
		http.Error(w, "Station not found", http.StatusNotFound)
		return
	}

	// SSRF protection
	if h.cfg.Proxy.BlockPrivateIPs {
		if blocked, reason := isPrivateURL(station.ListenURL); blocked {
			log.Printf("[WARN] SSRF blocked: station %d → %s (%s)", stationID, station.ListenURL, reason)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Determine buffer tier
	bufTier := r.URL.Query().Get("buf")
	if bufTier != "small" && bufTier != "medium" && bufTier != "large" && bufTier != "high" {
		bufTier = "small"
	}
	preBufSize := h.bufferSize(bufTier)

	// Client IP
	listenerIP := clientIP(r, h.cfg.Server.TrustProxy)

	// Generate connection ID
	connID := fmt.Sprintf("go_%d_%d", time.Now().UnixNano(), stationID)

	// Create stats logger
	sl := stats.New(h.pool.DB, connID)
	if err := sl.LogStart(stationID, station.ServerName, station.ListenURL,
		listenerIP, r.UserAgent(), bufTier, station.BitrateKbps); err != nil {
		log.Printf("[ERROR] logStart: %v", err)
	}

	// Track start time and peak kbps
	startTime := time.Now()
	var peakKbps float64
	var contentType string

	// Ensure logEnd fires on any exit path
	defer func() {
		dur := int(time.Since(startTime).Seconds())
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		memKB := int(memStats.Alloc / 1024)
		title, _ := sl.StreamTitle.Load().(string)
		sl.LogEnd(sl.BytesSent.Load(), dur, peakKbps, memKB,
			contentType, title, int(sl.MetaChanges.Load()))
	}()

	// Check if this is an SSE metadata request
	if r.URL.Query().Get("meta") == "1" {
		h.serveSSEMeta(w, r, station, sl)
		return
	}

	// Connect to upstream
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "GET", station.ListenURL, nil)
	if err != nil {
		log.Printf("[ERROR] create upstream request: %v", err)
		http.Error(w, "Bad upstream URL", http.StatusBadGateway)
		return
	}
	upstreamReq.Header.Set("User-Agent", h.cfg.Proxy.UserAgent)
	upstreamReq.Header.Set("Icy-MetaData", "1")
	upstreamReq.Header.Set("Accept", "audio/*, application/ogg, */*")

	resp, err := h.client.Do(upstreamReq)
	if err != nil {
		log.Printf("[ERROR] upstream connect %s: %v", station.ListenURL, err)
		http.Error(w, "Upstream connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		log.Printf("[WARN] upstream %s returned %d", station.ListenURL, resp.StatusCode)
		http.Error(w, "Upstream error", http.StatusBadGateway)
		return
	}

	// Parse ICY headers
	icyHdrs := icy.ParseHeaders(resp)

	// Log ICY headers to DB
	sl.LogIcyHeaders(icyHdrs.Name, icyHdrs.Bitrate, icyHdrs.Genre, icyHdrs.Metaint)

	// Capture content type
	contentType = resp.Header.Get("Content-Type")
	if ct := strings.SplitN(contentType, ";", 2); len(ct) > 0 {
		contentType = strings.TrimSpace(ct[0])
	}

	// Create ICY metadata stripper
	stripper := icy.NewStripper(icyHdrs.Metaint, func(title string) {
		sl.StreamTitle.Store(title)
		sl.MetaChanges.Add(1)
	})

	// Start stats ticker goroutine
	done := make(chan struct{})
	defer close(done)
	go sl.Ticker(done, startTime, &peakKbps, &contentType,
		time.Duration(h.cfg.Proxy.StatsUpdateSecs)*time.Second)

	// --- Send response headers to client ---
	wh := w.Header()
	if contentType != "" {
		wh.Set("Content-Type", contentType)
	} else {
		wh.Set("Content-Type", "audio/mpeg")
	}
	wh.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	wh.Set("Access-Control-Allow-Origin", "*")
	wh.Set("X-Proxy-Buffer-Tier", bufTier)
	wh.Set("X-Proxy-Buffer-Bytes", strconv.Itoa(preBufSize))
	wh.Set("X-Proxied-By", "CasterClub-GoProxy/1.0")
	if icyHdrs.Metaint > 0 {
		wh.Set("X-Proxy-ICY-MetaStrip", "1")
	}

	// Forward ICY headers to client (except metaint)
	for k, v := range icyHdrs.ForwardableHeaders() {
		wh.Set(k, v)
	}

	// Hijack for streaming (needed for flushing without chunked-encoding overhead)
	flusher, canFlush := w.(http.Flusher)

	// --- Pre-buffer phase ---
	preBuf := make([]byte, 0, preBufSize)
	readBuf := make([]byte, 32768) // 32KB read chunks from upstream

	// Bandwidth tracking
	windowBytes := 0
	windowStart := time.Now()

	for len(preBuf) < preBufSize {
		n, err := resp.Body.Read(readBuf)
		if n > 0 {
			audio := stripper.Strip(readBuf[:n])
			preBuf = append(preBuf, audio...)
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[WARN] upstream read during prebuffer: %v", err)
			}
			break
		}
	}

	// Write pre-buffer
	w.WriteHeader(http.StatusOK)
	if len(preBuf) > 0 {
		written, err := w.Write(preBuf)
		if err != nil {
			return
		}
		sl.BytesSent.Add(int64(written))
	}
	if canFlush {
		flusher.Flush()
	}
	preBuf = nil // release memory

	// --- Streaming phase ---
	for {
		n, readErr := resp.Body.Read(readBuf)
		if n > 0 {
			audio := stripper.Strip(readBuf[:n])
			if len(audio) > 0 {
				written, writeErr := w.Write(audio)
				if writeErr != nil {
					return // client disconnected
				}
				sl.BytesSent.Add(int64(written))
				windowBytes += written

				if canFlush {
					flusher.Flush()
				}
			}
		}

		// Peak bandwidth tracking (5-second windows)
		elapsed := time.Since(windowStart).Seconds()
		if elapsed >= 5.0 {
			kbps := (float64(windowBytes) / elapsed) / 125.0
			if kbps > peakKbps {
				peakKbps = kbps
			}
			windowBytes = 0
			windowStart = time.Now()
		}

		if readErr != nil {
			if readErr != io.EOF {
				log.Printf("[WARN] upstream read: %v", readErr)
			}
			return
		}

		// Check if client disconnected
		if r.Context().Err() != nil {
			return
		}
	}
}

// serveSSEMeta opens a separate upstream connection and streams metadata
// changes as Server-Sent Events.
func (h *Handler) serveSSEMeta(w http.ResponseWriter, r *http.Request,
	station *db.StationInfo, sl *stats.Logger) {

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	wh := w.Header()
	wh.Set("Content-Type", "text/event-stream")
	wh.Set("Cache-Control", "no-cache")
	wh.Set("X-Accel-Buffering", "no")
	wh.Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	// Connect to upstream for metadata
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upReq, err := http.NewRequestWithContext(ctx, "GET", station.ListenURL, nil)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonStr(err.Error()))
		flusher.Flush()
		return
	}
	upReq.Header.Set("User-Agent", "CasterClub-MetaPoll/1.0 (ICY2/2.2)")
	upReq.Header.Set("Icy-MetaData", "1")
	upReq.Header.Set("Accept", "*/*")

	resp, err := h.client.Do(upReq)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonStr(err.Error()))
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	icyHdrs := icy.ParseHeaders(resp)
	if icyHdrs.Metaint <= 0 {
		fmt.Fprintf(w, "data: {\"info\":\"no icy metadata from upstream\"}\n\n")
		flusher.Flush()
		// Keep alive with heartbeats until client disconnects
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	}

	// Create stripper that emits SSE on title change
	lastEmit := time.Now()
	stripper := icy.NewStripper(icyHdrs.Metaint, func(title string) {
		data, _ := json.Marshal(map[string]string{"title": title})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		lastEmit = time.Now()
	})

	// Read and discard audio, only care about metadata
	buf := make([]byte, 16384)
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		// Non-blocking heartbeat check
		select {
		case <-heartbeatTicker.C:
			if time.Since(lastEmit) >= 15*time.Second {
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
				lastEmit = time.Now()
			}
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			_ = stripper.Strip(buf[:n]) // discard audio, just trigger callbacks
		}
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonStr(err.Error()))
				flusher.Flush()
			}
			return
		}
		if r.Context().Err() != nil {
			return
		}
	}
}

// ServeHealth returns a JSON health check.
func (h *Handler) ServeHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "ok",
		"service":        "Mcaster1StreamProxy",
		"version":        "1.0.0",
		"active_streams": h.active.Load(),
		"max_streams":    h.cfg.Server.MaxConcurrentStreams,
		"uptime_secs":    0, // set by server
	})
}

// ServeStats returns current proxy statistics as JSON.
func (h *Handler) ServeStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_streams": h.active.Load(),
		"max_streams":    h.cfg.Server.MaxConcurrentStreams,
	})
}

// --- helpers ---

func (h *Handler) bufferSize(tier string) int {
	switch tier {
	case "medium":
		return h.cfg.Proxy.BufferMedium
	case "large":
		return h.cfg.Proxy.BufferLarge
	case "high":
		return h.cfg.Proxy.BufferHigh
	default:
		return h.cfg.Proxy.BufferSmall
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func isPrivateURL(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true, "invalid URL"
	}

	host := u.Hostname()
	if host == "" {
		return true, "empty host"
	}

	// Resolve DNS
	ips, err := net.LookupIP(host)
	if err != nil {
		return true, "DNS resolution failed"
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true, fmt.Sprintf("resolved to private IP %s", ip)
		}
	}

	return false, ""
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
