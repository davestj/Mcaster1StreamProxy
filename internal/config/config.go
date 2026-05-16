// Package config loads and validates the YAML configuration for Mcaster1StreamProxy.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	ListenAddr          string `yaml:"listen_addr"`
	ListenPort          int    `yaml:"listen_port"`
	RunAsUser           string `yaml:"run_as_user"`
	TLSCert             string `yaml:"tls_cert"`
	TLSKey              string `yaml:"tls_key"`
	MaxConcurrentStreams int    `yaml:"max_concurrent_streams"`
	ShutdownTimeoutSecs int    `yaml:"shutdown_timeout_secs"`
	TrustProxy          bool   `yaml:"trust_proxy"`
}

type DatabaseConfig struct {
	Host               string `yaml:"host"`
	Port               int    `yaml:"port"`
	User               string `yaml:"user"`
	Password           string `yaml:"password"`
	Database           string `yaml:"database"`
	Charset            string `yaml:"charset"`
	Collation          string `yaml:"collation"`
	MaxOpenConns       int    `yaml:"max_open_conns"`
	MaxIdleConns       int    `yaml:"max_idle_conns"`
	ConnMaxLifetimeSec int    `yaml:"conn_max_lifetime_secs"`
}

type ProxyConfig struct {
	BufferSmall       int    `yaml:"buffer_small"`
	BufferMedium      int    `yaml:"buffer_medium"`
	BufferLarge       int    `yaml:"buffer_large"`
	BufferHigh        int    `yaml:"buffer_high"`
	ConnectTimeoutSec int    `yaml:"connect_timeout_secs"`
	MaxRedirects      int    `yaml:"max_redirects"`
	StatsUpdateSecs   int    `yaml:"stats_update_secs"`
	RequestICYMeta    bool   `yaml:"request_icy_metadata"`
	UserAgent         string `yaml:"user_agent"`
	BlockPrivateIPs   bool   `yaml:"block_private_ips"`
}

type LoggingConfig struct {
	Dir        string `yaml:"dir"`
	Level      string `yaml:"level"`
	AccessLog  string `yaml:"access_log"`
	ErrorLog   string `yaml:"error_log"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Proxy    ProxyConfig    `yaml:"proxy"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// Load reads and parses the YAML config file, applying defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	applyDefaults(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func applyDefaults(c *Config) {
	if c.Server.ListenAddr == "" {
		c.Server.ListenAddr = "0.0.0.0"
	}
	if c.Server.ListenPort == 0 {
		c.Server.ListenPort = 9877
	}
	if c.Server.MaxConcurrentStreams == 0 {
		c.Server.MaxConcurrentStreams = 5000
	}
	if c.Server.ShutdownTimeoutSecs == 0 {
		c.Server.ShutdownTimeoutSecs = 30
	}
	if c.Database.Port == 0 {
		c.Database.Port = 3306
	}
	if c.Database.Charset == "" {
		c.Database.Charset = "utf8mb4"
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 10
	}
	if c.Database.ConnMaxLifetimeSec == 0 {
		c.Database.ConnMaxLifetimeSec = 300
	}
	if c.Proxy.BufferSmall == 0 {
		c.Proxy.BufferSmall = 16384
	}
	if c.Proxy.BufferMedium == 0 {
		c.Proxy.BufferMedium = 32768
	}
	if c.Proxy.BufferLarge == 0 {
		c.Proxy.BufferLarge = 57344
	}
	if c.Proxy.BufferHigh == 0 {
		c.Proxy.BufferHigh = 65536
	}
	if c.Proxy.ConnectTimeoutSec == 0 {
		c.Proxy.ConnectTimeoutSec = 8
	}
	if c.Proxy.MaxRedirects == 0 {
		c.Proxy.MaxRedirects = 5
	}
	if c.Proxy.StatsUpdateSecs == 0 {
		c.Proxy.StatsUpdateSecs = 10
	}
	if c.Proxy.UserAgent == "" {
		c.Proxy.UserAgent = "CasterClub-GoProxy/1.0 (ICY2/2.2; MetaStrip)"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.AccessLog == "" {
		c.Logging.AccessLog = "access.log"
	}
	if c.Logging.ErrorLog == "" {
		c.Logging.ErrorLog = "error.log"
	}
	if c.Logging.MaxSizeMB == 0 {
		c.Logging.MaxSizeMB = 100
	}
	if c.Logging.MaxBackups == 0 {
		c.Logging.MaxBackups = 5
	}
}

func validate(c *Config) error {
	if c.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database.user is required")
	}
	if c.Database.Database == "" {
		return fmt.Errorf("database.database is required")
	}
	if c.Server.TLSCert == "" || c.Server.TLSKey == "" {
		return fmt.Errorf("server.tls_cert and server.tls_key are required")
	}
	return nil
}

// DSN returns the MySQL DSN string for go-sql-driver/mysql.
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local&collation=%s",
		d.User, d.Password, d.Host, d.Port, d.Database, d.Charset, d.Collation)
}
