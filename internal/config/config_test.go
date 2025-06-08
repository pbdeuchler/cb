package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set required environment variables
	os.Setenv("SLACK_SIGNING_SECRET", "test-signing-secret")
	os.Setenv("SLACK_BOT_TOKEN", "xoxb-test-bot-token")
	
	defer func() {
		os.Unsetenv("SLACK_SIGNING_SECRET")
		os.Unsetenv("SLACK_BOT_TOKEN")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Test default values
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Database.Path != "./cb.db" {
		t.Errorf("Expected default database path './cb.db', got %s", cfg.Database.Path)
	}

	if cfg.Session.WorkDir != "./sessions" {
		t.Errorf("Expected default work dir './sessions', got %s", cfg.Session.WorkDir)
	}

	// Test required values
	if cfg.Slack.SigningSecret != "test-signing-secret" {
		t.Errorf("Expected slack signing secret 'test-signing-secret', got %s", cfg.Slack.SigningSecret)
	}

	if cfg.Slack.BotToken != "xoxb-test-bot-token" {
		t.Errorf("Expected slack bot token 'xoxb-test-bot-token', got %s", cfg.Slack.BotToken)
	}

}

func TestLoadWithCustomValues(t *testing.T) {
	// Set custom environment variables
	os.Setenv("PORT", "9090")
	os.Setenv("DB_PATH", "/tmp/test.db")
	os.Setenv("WORK_DIR", "/tmp/sessions")
	os.Setenv("MAX_SESSIONS_PER_USER", "10")
	os.Setenv("SESSION_IDLE_TIMEOUT", "7200")
	os.Setenv("SLACK_SIGNING_SECRET", "custom-signing-secret")
	os.Setenv("SLACK_BOT_TOKEN", "xoxb-custom-bot-token")
	os.Setenv("METRICS_ENABLED", "false")
	os.Setenv("LOG_LEVEL", "debug")
	
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DB_PATH")
		os.Unsetenv("WORK_DIR")
		os.Unsetenv("MAX_SESSIONS_PER_USER")
		os.Unsetenv("SESSION_IDLE_TIMEOUT")
		os.Unsetenv("SLACK_SIGNING_SECRET")
		os.Unsetenv("SLACK_BOT_TOKEN")
		os.Unsetenv("METRICS_ENABLED")
		os.Unsetenv("LOG_LEVEL")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Expected custom port 9090, got %d", cfg.Server.Port)
	}

	if cfg.Database.Path != "/tmp/test.db" {
		t.Errorf("Expected custom database path '/tmp/test.db', got %s", cfg.Database.Path)
	}

	if cfg.Session.WorkDir != "/tmp/sessions" {
		t.Errorf("Expected custom work dir '/tmp/sessions', got %s", cfg.Session.WorkDir)
	}

	if cfg.Session.MaxPerUser != 10 {
		t.Errorf("Expected custom max sessions 10, got %d", cfg.Session.MaxPerUser)
	}

	if cfg.Session.IdleTimeout != 7200 {
		t.Errorf("Expected custom idle timeout 7200, got %d", cfg.Session.IdleTimeout)
	}

	if cfg.Monitoring.MetricsEnabled != false {
		t.Errorf("Expected metrics disabled, got %v", cfg.Monitoring.MetricsEnabled)
	}

	if cfg.Monitoring.LogLevel != "debug" {
		t.Errorf("Expected log level 'debug', got %s", cfg.Monitoring.LogLevel)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	// Clear required environment variables
	os.Unsetenv("SLACK_SIGNING_SECRET")
	os.Unsetenv("SLACK_BOT_TOKEN") 

	_, err := Load()
	if err == nil {
		t.Error("Expected error when required environment variables are missing")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Server: struct {
					Port         int `env:"PORT" envDefault:"8080"`
					ReadTimeout  int `env:"READ_TIMEOUT" envDefault:"30"`
					WriteTimeout int `env:"WRITE_TIMEOUT" envDefault:"30"`
				}{
					Port: 8080,
				},
				Session: struct {
					WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
					MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
					IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
					ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude-code"`
				}{
					MaxPerUser:  5,
					IdleTimeout: 3600,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid port - too low",
			config: &Config{
				Server: struct {
					Port         int `env:"PORT" envDefault:"8080"`
					ReadTimeout  int `env:"READ_TIMEOUT" envDefault:"30"`
					WriteTimeout int `env:"WRITE_TIMEOUT" envDefault:"30"`
				}{
					Port: -1,
				},
				Session: struct {
					WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
					MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
					IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
					ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude-code"`
				}{
					MaxPerUser:  5,
					IdleTimeout: 3600,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port - too high",
			config: &Config{
				Server: struct {
					Port         int `env:"PORT" envDefault:"8080"`
					ReadTimeout  int `env:"READ_TIMEOUT" envDefault:"30"`
					WriteTimeout int `env:"WRITE_TIMEOUT" envDefault:"30"`
				}{
					Port: 70000,
				},
				Session: struct {
					WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
					MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
					IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
					ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude-code"`
				}{
					MaxPerUser:  5,
					IdleTimeout: 3600,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid max sessions",
			config: &Config{
				Server: struct {
					Port         int `env:"PORT" envDefault:"8080"`
					ReadTimeout  int `env:"READ_TIMEOUT" envDefault:"30"`
					WriteTimeout int `env:"WRITE_TIMEOUT" envDefault:"30"`
				}{
					Port: 8080,
				},
				Session: struct {
					WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
					MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
					IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
					ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude-code"`
				}{
					MaxPerUser:  0,
					IdleTimeout: 3600,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid idle timeout",
			config: &Config{
				Server: struct {
					Port         int `env:"PORT" envDefault:"8080"`
					ReadTimeout  int `env:"READ_TIMEOUT" envDefault:"30"`
					WriteTimeout int `env:"WRITE_TIMEOUT" envDefault:"30"`
				}{
					Port: 8080,
				},
				Session: struct {
					WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
					MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
					IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
					ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude-code"`
				}{
					MaxPerUser:  5,
					IdleTimeout: -1,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}