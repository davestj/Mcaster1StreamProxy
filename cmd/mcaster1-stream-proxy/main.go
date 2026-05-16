// Mcaster1StreamProxy — High-performance ICY stream proxy for CasterClub YP Directory
//
// Proxies HTTP audio/video streams through HTTPS, stripping inline ICY metadata.
// Designed to handle 5,000+ concurrent listeners on modest hardware.
//
// Usage:
//
//	mcaster1-stream-proxy -c /path/to/config.yaml
//
// Owner: MCaster1 LLC / David St John <davestj@mcaster1.com>
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mcaster1/Mcaster1StreamProxy/internal/config"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/db"
	"github.com/mcaster1/Mcaster1StreamProxy/internal/server"
)

var (
	version   = "1.0.0"
	buildTime = "dev"
)

func main() {
	configPath := flag.String("c", "", "Path to config.yaml")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Mcaster1StreamProxy v%s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Find config file
	cfgFile := *configPath
	if cfgFile == "" {
		// Try common locations
		candidates := []string{
			"etc/config.yaml",
			"../etc/config.yaml",
			"/var/www/mcaster1.com/Mcaster1StreamProxy/etc/config.yaml",
			"/usr/local/mcaster1/etc/stream-proxy.yaml",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				cfgFile = c
				break
			}
		}
		if cfgFile == "" {
			log.Fatal("[FATAL] No config file found. Use -c /path/to/config.yaml")
		}
	}

	absPath, _ := filepath.Abs(cfgFile)
	log.Printf("[INFO] Loading config: %s", absPath)

	cfg, err := config.Load(cfgFile)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	// Set up log directory
	if cfg.Logging.Dir != "" {
		if err := os.MkdirAll(cfg.Logging.Dir, 0755); err != nil {
			log.Printf("[WARN] Could not create log dir %s: %v", cfg.Logging.Dir, err)
		}

		logFile := filepath.Join(cfg.Logging.Dir, cfg.Logging.ErrorLog)
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("[WARN] Could not open log file %s: %v", logFile, err)
		} else {
			log.SetOutput(f)
			log.Printf("[INFO] Logging to %s", logFile)
		}
	}

	// Connect to database
	log.Printf("[INFO] Connecting to MariaDB %s@%s:%d/%s",
		cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database)

	pool, err := db.New(&cfg.Database)
	if err != nil {
		log.Fatalf("[FATAL] Database connection failed: %v", err)
	}
	defer pool.Close()
	log.Printf("[INFO] Database pool ready (max_open=%d, max_idle=%d)",
		cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)

	// Create server
	srv := server.New(cfg, pool)

	// Signal handling — graceful shutdown on SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("[INFO] Received signal %v — initiating graceful shutdown", sig)
		if err := srv.Shutdown(); err != nil {
			log.Printf("[ERROR] Shutdown error: %v", err)
		}
		os.Exit(0)
	}()

	// Start serving
	if err := srv.Start(); err != nil {
		log.Fatalf("[FATAL] Server error: %v", err)
	}
}
