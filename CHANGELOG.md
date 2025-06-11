# Changelog

All notable changes to Claude Bot will be documented in this file by Claude Bot themselves.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.0.1] - 2025-01-10

### Added

- Initial release of Claude Bot
- Slack integration for Claude Code session management
- Multi-platform binary builds (Linux AMD64/ARM64, macOS Intel/Apple Silicon, Windows)
- Session management with SQLite database storage
- Git repository operations and worktree management
- Encrypted credential storage for API keys and tokens
- Prometheus metrics and health monitoring
- Thread-based session isolation in Slack
- GitHub Actions CI/CD pipeline with automated releases

### Features

- `@cb start` - Start new Claude Code sessions from Git repositories
- `@cb stop` - End active sessions
- `@cb status` - Check session status
- `@cb list` - List active sessions
- `@cb credentials` - Manage API keys and tokens
- `@cb help` - Command documentation

### Security

- Basic encryption for stored credentials
- Slack request signature verification support
- Session isolation per user and thread

### Notes

- This is an experimental release created primarily by Claude Code
- Not production-ready - contains security limitations
- Credentials stored with basic encryption only
- Requires manual Slack app setup and configuration
