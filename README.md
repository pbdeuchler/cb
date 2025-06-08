# Claude Bot (CB)

A distributed service that bridges Slack workspaces with Claude Code sessions, enabling collaborative AI-assisted development directly within Slack channels and threads.

## Features

- **Slack Integration**: Interact with Claude Code through Slack mentions and commands
- **Session Management**: Create, manage, and monitor Claude Code sessions
- **Repository Operations**: Clone repositories, manage work trees, commit and push changes
- **Credential Storage**: Simple storage of API keys and tokens
- **Multi-tenancy**: Support for multiple Slack workspaces and users
- **Metrics & Monitoring**: Prometheus metrics and health checks
- **Thread Support**: Sessions can be isolated to specific Slack threads

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Slack API     │────▶│   CB Service     │────▶│  Claude Code    │
│   (Webhooks)    │     │   (Go/SQLite)    │     │   (Headless)    │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                               │
                               ▼
                        ┌──────────────────┐
                        │   Git Repos      │
                        │   MCP Servers    │
                        └──────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21 or later
- Claude Code CLI installed
- Git
- Slack Bot Token and Signing Secret

### Installation

1. Clone the repository:
```bash
git clone https://github.com/pbdeuchler/claude-bot
cd claude-bot
```

2. Build the application:
```bash
go build -o cb ./cmd/cb
```

3. Set up environment variables:
```bash
export SLACK_BOT_TOKEN="xoxb-your-bot-token"
export SLACK_SIGNING_SECRET="your-signing-secret"
```

4. Run the service:
```bash
./cb
```

## Configuration

The service is configured via environment variables:

### Required Variables

- `SLACK_BOT_TOKEN`: Your Slack bot token
- `SLACK_SIGNING_SECRET`: Your Slack signing secret

### Optional Variables

- `PORT`: HTTP server port (default: 8080)
- `DB_PATH`: SQLite database path (default: ./cb.db)
- `WORK_DIR`: Session work directory (default: ./sessions)
- `MAX_SESSIONS_PER_USER`: Maximum sessions per user (default: 5)
- `SESSION_IDLE_TIMEOUT`: Session idle timeout in seconds (default: 3600)
- `CLAUDE_CODE_PATH`: Path to claude-code binary (default: claude-code)
- `METRICS_ENABLED`: Enable Prometheus metrics (default: true)
- `LOG_LEVEL`: Logging level (default: info)

## Slack Commands

### Starting a Session

```
@cb start <repo-url> [branch] [--thread]
```

Examples:
- `@cb start https://github.com/user/repo`
- `@cb start https://github.com/user/repo feature-branch`
- `@cb start https://github.com/user/repo main --thread`

### Managing Sessions

- `@cb stop` - End the current session in this channel/thread
- `@cb status` - Show current session status
- `@cb list` - List your active sessions

### Credentials

- `@cb credentials set anthropic sk-ant-...` - Set Anthropic API key
- `@cb credentials set github ghp_...` - Set GitHub token
- `@cb credentials list` - List stored credential types

### Help

- `@cb help` - Show available commands

## API Endpoints

- `GET /health` - Health check endpoint
- `POST /slack/events` - Slack events webhook
- `GET /metrics` - Prometheus metrics (if enabled)

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run specific test packages
go test ./internal/config ./internal/crypto ./internal/slack

# Run with coverage
go test -cover ./...
```

### Project Structure

```
cb/
├── cmd/cb/                 # Main application
├── internal/
│   ├── config/            # Configuration management
│   ├── crypto/            # Encryption/decryption
│   ├── db/                # Database layer and migrations
│   │   └── migrations/    # SQL migration files
│   ├── logging/           # Structured logging
│   ├── metrics/           # Prometheus metrics
│   ├── repo/              # Git repository operations
│   ├── session/           # Session and Claude process management
│   └── slack/             # Slack event handlers and parsers
├── pkg/models/            # Data models and types
└── test/                  # Integration tests
```

## Security Considerations

- User credentials are stored as plain text in the database
- Slack request signatures should be verified in production
- Use HTTPS in production environments
- Restrict access to the database file
- Regularly rotate API keys and tokens

## Monitoring

The service provides comprehensive metrics via Prometheus:

- Session lifecycle metrics
- Command processing metrics
- Error rates and types
- Claude process metrics
- Repository operation metrics
- Database operation metrics

Access metrics at `http://localhost:9090/metrics` (default).

## Deployment

### Docker

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o cb ./cmd/cb

FROM alpine:latest
RUN apk --no-cache add ca-certificates git
WORKDIR /root/
COPY --from=builder /app/cb .
CMD ["./cb"]
```

### Systemd Service

```ini
[Unit]
Description=Claude Bot Service
After=network.target

[Service]
Type=simple
User=claude-bot
WorkingDirectory=/opt/claude-bot
ExecStart=/opt/claude-bot/cb
Restart=always
Environment=SLACK_BOT_TOKEN=xoxb-your-token
Environment=SLACK_SIGNING_SECRET=your-secret

[Install]
WantedBy=multi-user.target
```

## Troubleshooting

### Common Issues

1. **Session creation fails**: Check that Claude Code is installed and accessible
2. **Repository access denied**: Ensure GitHub credentials are set and valid
3. **Database errors**: Check file permissions and disk space
4. **Slack events not received**: Verify webhook URL and signing secret

### Logs

The service provides structured logging. Set `LOG_LEVEL=debug` for detailed debugging information.

### Health Checks

Check service health:
```bash
curl http://localhost:8080/health
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Run the test suite
6. Create a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.