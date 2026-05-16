// Package db provides the MariaDB connection pool for Mcaster1StreamProxy.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/mcaster1/Mcaster1StreamProxy/internal/config"
)

// Pool wraps a *sql.DB with our config.
type Pool struct {
	DB *sql.DB
}

// New creates a new database connection pool from config.
func New(cfg *config.DatabaseConfig) (*Pool, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSec) * time.Second)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Pool{DB: db}, nil
}

// Close shuts down the connection pool.
func (p *Pool) Close() error {
	return p.DB.Close()
}

// StationInfo holds the fields we need from the stations table.
type StationInfo struct {
	ID          int
	ServerName  string
	ListenURL   string
	BitrateKbps int
}

// LookupStation fetches a station by ID, returns nil if not found or deleted.
func (p *Pool) LookupStation(id int) (*StationInfo, error) {
	row := p.DB.QueryRow(
		`SELECT id, server_name, listen_url,
		        CAST(COALESCE(NULLIF(TRIM(bitrate_kbps),''), '0') AS UNSIGNED) AS bitrate_kbps
		 FROM stations
		 WHERE id = ? AND (flag_deleted = 0 OR flag_deleted IS NULL)
		 LIMIT 1`, id)

	s := &StationInfo{}
	err := row.Scan(&s.ID, &s.ServerName, &s.ListenURL, &s.BitrateKbps)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup station %d: %w", id, err)
	}
	return s, nil
}
