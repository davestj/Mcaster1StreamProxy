// Package server wires up the HTTP routes and manages the TLS listener.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mcaster1/Mcaster1StreamProxy/internal/config"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/db"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/proxy"
)

// Server is the main application server.
type Server struct {
	cfg       *config.Config
	pool      *db.Pool
	handler   *proxy.Handler
	httpSrv   *http.Server
	startTime time.Time
}

// New creates a server from config and DB pool.
func New(cfg *config.Config, pool *db.Pool) *Server {
	return &Server{
		cfg:       cfg,
		pool:      pool,
		handler:   proxy.New(cfg, pool),
		startTime: time.Now(),
	}
}

// Start begins listening on the configured address with TLS.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Stream proxy — main endpoint
	mux.HandleFunc("/stream", s.handler.ServeStream)

	// Health check
	mux.HandleFunc("/health", s.healthWithUptime)

	// Stats
	mux.HandleFunc("/stats", s.handler.ServeStats)

	// Root — redirect to health
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/health", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	addr := fmt.Sprintf("%s:%d", s.cfg.Server.ListenAddr, s.cfg.Server.ListenPort)

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
	}

	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No read/write timeout — streams are infinite
	}

	log.Printf("[INFO] Mcaster1StreamProxy starting on https://%s", addr)
	log.Printf("[INFO] Max concurrent streams: %d", s.cfg.Server.MaxConcurrentStreams)
	log.Printf("[INFO] TLS cert: %s", s.cfg.Server.TLSCert)

	return s.httpSrv.ListenAndServeTLS(s.cfg.Server.TLSCert, s.cfg.Server.TLSKey)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(s.cfg.Server.ShutdownTimeoutSecs)*time.Second)
	defer cancel()

	log.Printf("[INFO] Shutting down (active streams: %d, timeout: %ds)...",
		s.handler.ActiveStreams(), s.cfg.Server.ShutdownTimeoutSecs)

	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) healthWithUptime(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	uptime := int(time.Since(s.startTime).Seconds())
	fmt.Fprintf(w, `{"status":"ok","service":"Mcaster1StreamProxy","version":"1.0.0","active_streams":%d,"max_streams":%d,"uptime_secs":%d}`,
		s.handler.ActiveStreams(), s.cfg.Server.MaxConcurrentStreams, uptime)
}
