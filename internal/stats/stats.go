// Package stats logs proxy connection metrics to ypman_proxy_stats.
// Mirrors the exact same table and column semantics as proxy-stream.php's
// ProxyStatsLogger, so both Go and PHP proxy stats appear in the same
// dashboard in Mcaster1YPMan.
package stats

import (
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Logger tracks a single proxy stream session in the database.
type Logger struct {
	db   *sql.DB
	mu   sync.Mutex
	rowID int64

	connID      string
	ctWritten   bool
	icyWritten  bool

	// Atomic counters for thread-safe access from the streaming goroutine
	BytesSent    atomic.Int64
	MetaChanges  atomic.Int32
	StreamTitle  atomic.Value // stores string
}

// New creates a Logger bound to the given DB pool.
func New(db *sql.DB, connID string) *Logger {
	l := &Logger{
		db:     db,
		connID: connID,
	}
	l.StreamTitle.Store("")
	return l
}

// RowID returns the auto-increment ID of this session's row.
func (l *Logger) RowID() int64 {
	return l.rowID
}

// ConnID returns the connection identifier.
func (l *Logger) ConnID() string {
	return l.connID
}

// LogStart inserts the initial row when a stream connection begins.
func (l *Logger) LogStart(stationID int, stationName, listenURL,
	listenerIP, userAgent, bufTier string, bitrateKbps int) error {

	mountPoint := ""
	if u, err := url.Parse(listenURL); err == nil {
		mountPoint = u.Path
	}

	res, err := l.db.Exec(
		`INSERT INTO ypman_proxy_stats
		 (conn_id, station_id, station_name, mount_point, listen_url,
		  listener_ip, user_agent, buf_tier, bitrate_kbps)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.connID, stationID, stationName, mountPoint, listenURL,
		listenerIP, userAgent, bufTier, bitrateKbps)
	if err != nil {
		return fmt.Errorf("logStart: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("logStart lastInsertId: %w", err)
	}
	l.rowID = id
	return nil
}

// LogIcyHeaders writes captured ICY response headers to the DB (called once).
func (l *Logger) LogIcyHeaders(icyName string, icyBr int, icyGenre string, icyMetaint int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rowID <= 0 || l.icyWritten {
		return
	}
	l.icyWritten = true

	if len(icyName) > 255 {
		icyName = icyName[:255]
	}
	if len(icyGenre) > 128 {
		icyGenre = icyGenre[:128]
	}

	_, _ = l.db.Exec(
		`UPDATE ypman_proxy_stats
		 SET icy_name=?, icy_br=?, icy_genre=?, icy_metaint=?, updated_at=NOW()
		 WHERE id=?`,
		icyName, icyBr, icyGenre, icyMetaint, l.rowID)
}

// LogProgress updates running stats (called every ~10s during streaming).
func (l *Logger) LogProgress(bytesSent int64, durationSecs int, peakKbps float64,
	contentType, streamTitle string, metaChanges int) {

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rowID <= 0 {
		return
	}

	avgKbps := 0.0
	if durationSecs > 0 {
		avgKbps = math.Round((float64(bytesSent)/float64(durationSecs))/125.0*10) / 10
	}
	peakKbps = math.Round(peakKbps*10) / 10

	if len(streamTitle) > 512 {
		streamTitle = streamTitle[:512]
	}

	if contentType != "" && !l.ctWritten {
		l.ctWritten = true
		if len(contentType) > 128 {
			contentType = contentType[:128]
		}
		_, _ = l.db.Exec(
			`UPDATE ypman_proxy_stats
			 SET bytes_sent=?, duration_secs=?, peak_kbps=?, avg_kbps=?,
			     content_type=?, last_stream_title=?, meta_changes=?, updated_at=NOW()
			 WHERE id=?`,
			bytesSent, durationSecs, peakKbps, avgKbps,
			contentType, streamTitle, metaChanges, l.rowID)
	} else {
		_, _ = l.db.Exec(
			`UPDATE ypman_proxy_stats
			 SET bytes_sent=?, duration_secs=?, peak_kbps=?, avg_kbps=?,
			     last_stream_title=?, meta_changes=?, updated_at=NOW()
			 WHERE id=?`,
			bytesSent, durationSecs, peakKbps, avgKbps,
			streamTitle, metaChanges, l.rowID)
	}
}

// LogEnd writes the final stats and marks the connection as ended.
func (l *Logger) LogEnd(bytesSent int64, durationSecs int, peakKbps float64,
	memPeakKB int, contentType, streamTitle string, metaChanges int) {

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rowID <= 0 {
		return
	}

	avgKbps := 0.0
	if durationSecs > 0 {
		avgKbps = math.Round((float64(bytesSent)/float64(durationSecs))/125.0*10) / 10
	}
	peakKbps = math.Round(peakKbps*10) / 10

	if len(contentType) > 128 {
		contentType = contentType[:128]
	}
	if len(streamTitle) > 512 {
		streamTitle = streamTitle[:512]
	}

	_, _ = l.db.Exec(
		`UPDATE ypman_proxy_stats
		 SET bytes_sent=?, duration_secs=?, peak_kbps=?, avg_kbps=?,
		     memory_peak_kb=?, content_type=?, last_stream_title=?, meta_changes=?,
		     ended=1, ended_at=NOW(), updated_at=NOW()
		 WHERE id=?`,
		bytesSent, durationSecs, peakKbps, avgKbps,
		memPeakKB, contentType, streamTitle, metaChanges, l.rowID)
}

// Ticker runs periodic LogProgress updates until the done channel is closed.
func (l *Logger) Ticker(done <-chan struct{}, startTime time.Time, peakKbps *float64,
	contentType *string, interval time.Duration) {

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			dur := int(time.Since(startTime).Seconds())
			title, _ := l.StreamTitle.Load().(string)
			l.LogProgress(
				l.BytesSent.Load(),
				dur,
				*peakKbps,
				*contentType,
				title,
				int(l.MetaChanges.Load()),
			)
		}
	}
}
