package config

import (
	"fmt"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	Server struct {
		Port         int `env:"PORT" envDefault:"8080"`
		ReadTimeout  int `env:"READ_TIMEOUT" envDefault:"30"`
		WriteTimeout int `env:"WRITE_TIMEOUT" envDefault:"30"`
	}

	Database struct {
		Path           string `env:"DB_PATH" envDefault:"./cb.db"`
		MaxConnections int    `env:"DB_MAX_CONN" envDefault:"10"`
	}

	Slack struct {
		SigningSecret string `env:"SLACK_SIGNING_SECRET,required"`
		BotToken      string `env:"SLACK_BOT_TOKEN,required"`
	}

	Session struct {
		WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
		MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
		IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
		ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude"`
	}

	Monitoring struct {
		MetricsEnabled bool   `env:"METRICS_ENABLED" envDefault:"true"`
		MetricsPort    int    `env:"METRICS_PORT" envDefault:"9090"`
		LogLevel       string `env:"LOG_LEVEL" envDefault:"info"`
	}
}

func Load() (*Config, error) {
	var cfg Config

	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Session.MaxPerUser <= 0 {
		return fmt.Errorf("max sessions per user must be positive")
	}

	if c.Session.IdleTimeout <= 0 {
		return fmt.Errorf("session idle timeout must be positive")
	}

	return nil
}

