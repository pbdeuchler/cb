# Claude-Bot (cb) Service Design & Implementation Guide

## Overview

Claude-Bot (`cb`) is a distributed service that bridges Slack workspaces with Claude code sessions, enabling collaborative AI-assisted development directly within Slack channels and threads. The service manages persistent coding sessions, repository operations, and user authentication across multiple Slack workspaces.

## Architecture

### Core Components

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

### Database Schema

```sql
-- Users table
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slack_workspace_id TEXT NOT NULL,
    slack_user_id TEXT NOT NULL,
    slack_user_name TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(slack_workspace_id, slack_user_id)
);

-- Credentials table
CREATE TABLE credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    credential_type TEXT NOT NULL CHECK(credential_type IN ('anthropic', 'github')),
    encrypted_value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Sessions table
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    session_id TEXT UNIQUE NOT NULL,
    slack_workspace_id TEXT NOT NULL,
    slack_channel_id TEXT NOT NULL,
    slack_thread_ts TEXT,
    repo_url TEXT NOT NULL,
    branch_name TEXT NOT NULL,
    work_tree_path TEXT NOT NULL,
    claude_process_pid INTEGER,
    status TEXT NOT NULL CHECK(status IN ('active', 'ending', 'ended', 'error')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Session messages table for audit trail
CREATE TABLE session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    slack_message_ts TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('user_to_claude', 'claude_to_user')),
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_users_slack ON users(slack_workspace_id, slack_user_id);
CREATE INDEX idx_sessions_active ON sessions(status) WHERE status = 'active';
CREATE INDEX idx_sessions_channel ON sessions(slack_workspace_id, slack_channel_id, slack_thread_ts);
```

## API Design

### Slack Event Handlers

```go
type SlackEventHandler interface {
    HandleAppMention(event *slack.AppMentionEvent) error
    HandleMessage(event *slack.MessageEvent) error
    HandleSlashCommand(cmd *slack.SlashCommand) error
}
```

### Core Service Interfaces

```go
type SessionManager interface {
    CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error)
    GetSession(ctx context.Context, sessionID string) (*Session, error)
    GetActiveSessionForChannel(ctx context.Context, workspaceID, channelID, threadTS string) (*Session, error)
    EndSession(ctx context.Context, sessionID string) error
    SendToSession(ctx context.Context, sessionID, message string) (string, error)
}

type CredentialManager interface {
    StoreCredential(ctx context.Context, userID int64, credType, value string) error
    GetCredential(ctx context.Context, userID int64, credType string) (string, error)
    HasRequiredCredentials(ctx context.Context, userID int64) (bool, error)
}

type RepositoryManager interface {
    CloneOrCreateWorkTree(ctx context.Context, repoURL, branch, workDir string) error
    CommitAndPush(ctx context.Context, workDir, branch, message string) error
    Cleanup(ctx context.Context, workDir string) error
}

type ClaudeCodeManager interface {
    StartSession(ctx context.Context, workDir string, apiKey string) (*ClaudeProcess, error)
    SendCommand(ctx context.Context, pid int, command string) (string, error)
    StopSession(ctx context.Context, pid int) error
}
```

### Request/Response Types

```go
type CreateSessionRequest struct {
    WorkspaceID string
    UserID      string
    ChannelID   string
    ThreadTS    string // empty for channel-pinned sessions
    RepoURL     string
    Branch      string
}

type Session struct {
    ID           string
    UserID       int64
    WorkspaceID  string
    ChannelID    string
    ThreadTS     string
    RepoURL      string
    Branch       string
    WorkTreePath string
    Status       string
    CreatedAt    time.Time
}
```

## Implementation Details

### Service Structure

```
cb/
├── cmd/
│   └── cb/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── db/
│   │   ├── migrations/
│   │   └── sqlite.go
│   ├── slack/
│   │   ├── handler.go
│   │   └── parser.go
│   ├── session/
│   │   ├── manager.go
│   │   └── claude.go
│   ├── repo/
│   │   └── git.go
│   └── crypto/
│       └── encrypt.go
├── pkg/
│   └── models/
│       └── types.go
├── go.mod
└── go.sum
```

### Key Implementation Functions

```go
// slack/parser.go
func ParseStartCommand(text string) (*StartCommandParams, error) {
    // Format: start <repo-url> [branch] [--thread]
    parts := strings.Fields(text)
    if len(parts) < 2 {
        return nil, fmt.Errorf("usage: @cb start <repo-url> [branch] [--thread]")
    }

    params := &StartCommandParams{
        RepoURL:    parts[1],
        Branch:     "main",
        UseThread:  false,
    }

    for i := 2; i < len(parts); i++ {
        if parts[i] == "--thread" {
            params.UseThread = true
        } else if !strings.HasPrefix(parts[i], "--") {
            params.Branch = parts[i]
        }
    }

    return params, nil
}

// session/manager.go
func (m *Manager) CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error) {
    // Check for existing active session
    existing, err := m.GetActiveSessionForChannel(ctx, req.WorkspaceID, req.ChannelID, req.ThreadTS)
    if err == nil && existing != nil {
        return nil, fmt.Errorf("active session already exists in this context")
    }

    // Validate channel restrictions
    if req.ChannelID == "general" {
        return nil, fmt.Errorf("sessions cannot be started in #general")
    }

    // Generate work tree path
    sessionID := generateSessionID()
    workTreePath := filepath.Join(m.workDir, sessionID)

    // Clone or create work tree
    if err := m.repoMgr.CloneOrCreateWorkTree(ctx, req.RepoURL, req.Branch, workTreePath); err != nil {
        return nil, fmt.Errorf("failed to setup repository: %w", err)
    }

    // Start Claude Code session
    apiKey, err := m.credMgr.GetCredential(ctx, req.UserID, "anthropic")
    if err != nil {
        return nil, fmt.Errorf("missing Anthropic credentials")
    }

    process, err := m.claudeMgr.StartSession(ctx, workTreePath, apiKey)
    if err != nil {
        return nil, fmt.Errorf("failed to start Claude session: %w", err)
    }

    // Store session in database
    session := &Session{
        ID:           sessionID,
        UserID:       req.UserID,
        WorkspaceID:  req.WorkspaceID,
        ChannelID:    req.ChannelID,
        ThreadTS:     req.ThreadTS,
        RepoURL:      req.RepoURL,
        Branch:       req.Branch,
        WorkTreePath: workTreePath,
        ProcessPID:   process.PID,
        Status:       "active",
    }

    if err := m.db.CreateSession(ctx, session); err != nil {
        m.claudeMgr.StopSession(ctx, process.PID)
        return nil, fmt.Errorf("failed to store session: %w", err)
    }

    return session, nil
}

// session/claude.go
func (c *ClaudeManager) StartSession(ctx context.Context, workDir string, apiKey string) (*ClaudeProcess, error) {
    cmd := exec.CommandContext(ctx, "claude-code",
        "--headless",
        "--api-key", apiKey,
        "--work-dir", workDir,
        "--enable-mcp-servers",
    )

    stdin, err := cmd.StdinPipe()
    if err != nil {
        return nil, err
    }

    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return nil, err
    }

    if err := cmd.Start(); err != nil {
        return nil, err
    }

    return &ClaudeProcess{
        PID:    cmd.Process.Pid,
        Stdin:  stdin,
        Stdout: stdout,
        Cmd:    cmd,
    }, nil
}
```

### Security Considerations

```go
// crypto/encrypt.go
func EncryptCredential(plaintext, key string) (string, error) {
    // Use AES-256-GCM for credential encryption
    block, err := aes.NewCipher([]byte(key))
    if err != nil {
        return "", err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }

    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}
```

### Session Lifecycle Management

```go
func (m *Manager) EndSession(ctx context.Context, sessionID string) error {
    session, err := m.GetSession(ctx, sessionID)
    if err != nil {
        return err
    }

    if session.Status != "active" {
        return fmt.Errorf("session is not active")
    }

    // Update status to ending
    if err := m.db.UpdateSessionStatus(ctx, sessionID, "ending"); err != nil {
        return err
    }

    // Stop Claude process
    if err := m.claudeMgr.StopSession(ctx, session.ProcessPID); err != nil {
        log.Printf("failed to stop Claude process: %v", err)
    }

    // Commit and push changes
    commitMsg := fmt.Sprintf("CB Session %s changes", sessionID)
    if err := m.repoMgr.CommitAndPush(ctx, session.WorkTreePath, session.Branch, commitMsg); err != nil {
        log.Printf("failed to commit changes: %v", err)
    }

    // Cleanup work tree
    if err := m.repoMgr.Cleanup(ctx, session.WorkTreePath); err != nil {
        log.Printf("failed to cleanup work tree: %v", err)
    }

    // Update status to ended
    return m.db.UpdateSessionStatus(ctx, sessionID, "ended")
}
```

## Configuration

```go
type Config struct {
    Server struct {
        Port            int    `env:"PORT" envDefault:"8080"`
        ReadTimeout     int    `env:"READ_TIMEOUT" envDefault:"30"`
        WriteTimeout    int    `env:"WRITE_TIMEOUT" envDefault:"30"`
    }

    Database struct {
        Path            string `env:"DB_PATH" envDefault:"./cb.db"`
        MaxConnections  int    `env:"DB_MAX_CONN" envDefault:"10"`
    }

    Slack struct {
        SigningSecret   string `env:"SLACK_SIGNING_SECRET,required"`
        BotToken        string `env:"SLACK_BOT_TOKEN,required"`
    }

    Security struct {
        EncryptionKey   string `env:"ENCRYPTION_KEY,required"`
    }

    Session struct {
        WorkDir         string `env:"WORK_DIR" envDefault:"./sessions"`
        MaxPerUser      int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
        IdleTimeout     int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
    }
}
```

## Error Handling

```go
type CBError struct {
    Code    string
    Message string
    Err     error
}

const (
    ErrCodeInvalidCommand     = "INVALID_COMMAND"
    ErrCodeSessionExists      = "SESSION_EXISTS"
    ErrCodeNoCredentials      = "NO_CREDENTIALS"
    ErrCodeClaudeUnavailable  = "CLAUDE_UNAVAILABLE"
    ErrCodeRepoAccess         = "REPO_ACCESS"
)

func NewCBError(code, message string, err error) *CBError {
    return &CBError{
        Code:    code,
        Message: message,
        Err:     err,
    }
}
```

## Deployment Considerations

### Health Checks

```go
func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
    checks := map[string]bool{
        "database": s.checkDatabase(),
        "slack":    s.checkSlackConnection(),
        "storage":  s.checkStorageAccess(),
    }

    healthy := true
    for _, ok := range checks {
        if !ok {
            healthy = false
            break
        }
    }

    status := http.StatusOK
    if !healthy {
        status = http.StatusServiceUnavailable
    }

    w.WriteHeader(status)
    json.NewEncoder(w).Encode(checks)
}
```

### Graceful Shutdown

```go
func (s *Server) Start() error {
    srv := &http.Server{
        Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
        Handler:      s.router,
        ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
        WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
    }

    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("listen: %s\n", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Println("Shutting down server...")

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // End all active sessions
    if err := s.sessionMgr.EndAllActiveSessions(ctx); err != nil {
        log.Printf("Error ending sessions: %v", err)
    }

    return srv.Shutdown(ctx)
}
```

## Testing Strategy

### Unit Tests

```go
func TestParseStartCommand(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *StartCommandParams
        wantErr bool
    }{
        {
            name:  "basic repo",
            input: "start https://github.com/user/repo",
            want: &StartCommandParams{
                RepoURL:   "https://github.com/user/repo",
                Branch:    "main",
                UseThread: false,
            },
        },
        {
            name:  "with branch and thread",
            input: "start https://github.com/user/repo feature-branch --thread",
            want: &StartCommandParams{
                RepoURL:   "https://github.com/user/repo",
                Branch:    "feature-branch",
                UseThread: true,
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseStartCommand(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseStartCommand() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("ParseStartCommand() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Integration Tests

```go
func TestSessionLifecycle(t *testing.T) {
    ctx := context.Background()
    mgr := setupTestManager(t)

    // Create session
    req := CreateSessionRequest{
        WorkspaceID: "W123",
        UserID:      "U456",
        ChannelID:   "C789",
        RepoURL:     "https://github.com/test/repo",
        Branch:      "main",
    }

    session, err := mgr.CreateSession(ctx, req)
    require.NoError(t, err)
    require.NotEmpty(t, session.ID)

    // Send command
    response, err := mgr.SendToSession(ctx, session.ID, "ls -la")
    require.NoError(t, err)
    require.NotEmpty(t, response)

    // End session
    err = mgr.EndSession(ctx, session.ID)
    require.NoError(t, err)

    // Verify session ended
    s, err := mgr.GetSession(ctx, session.ID)
    require.NoError(t, err)
    require.Equal(t, "ended", s.Status)
}
```

## Edge Cases & Error Scenarios

1. **Duplicate Sessions**: Prevent multiple active sessions in same channel/thread
2. **Channel Restrictions**: Block sessions in channels like #general
3. **Credential Expiry**: Handle expired GitHub/Anthropic tokens gracefully
4. **Process Crashes**: Detect and clean up orphaned Claude processes
5. **Network Partitions**: Handle Slack API timeouts and retries
6. **Repository Access**: Handle private repos, invalid URLs, network issues
7. **Storage Limits**: Monitor and enforce work directory size limits
8. **Concurrent Access**: Handle multiple users in same channel/thread
9. **Message Ordering**: Ensure commands are processed in order
10. **Cleanup Failures**: Gracefully handle failures during session cleanup

## Monitoring & Observability

```go
type Metrics struct {
    SessionsCreated   prometheus.Counter
    SessionsEnded     prometheus.Counter
    SessionDuration   prometheus.Histogram
    CommandsProcessed prometheus.Counter
    ErrorsTotal       prometheus.CounterVec
}

func (m *Manager) recordSessionMetrics(session *Session) {
    m.metrics.SessionDuration.Observe(time.Since(session.CreatedAt).Seconds())
    m.metrics.SessionsEnded.Inc()
}
```

## Future Enhancements

1. **Session Sharing**: Allow multiple users to interact with same session
2. **Session Templates**: Pre-configured environments for common workflows
3. **Artifact Storage**: Store and retrieve session outputs
4. **Rate Limiting**: Per-user and per-workspace limits
5. **Session Migration**: Move sessions between channels/threads
6. **Backup/Restore**: Session state persistence
7. **Multi-Region**: Deploy across regions for lower latency
8. **WebSocket Support**: Real-time updates instead of polling
