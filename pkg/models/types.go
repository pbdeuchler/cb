package models

import (
	"fmt"
	"time"
)

// User represents a user in the system
type User struct {
	ID               int64     `json:"id" db:"id"`
	SlackWorkspaceID string    `json:"slack_workspace_id" db:"slack_workspace_id"`
	SlackUserID      string    `json:"slack_user_id" db:"slack_user_id"`
	SlackUserName    string    `json:"slack_user_name" db:"slack_user_name"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// Credential represents user credentials
type Credential struct {
	ID              int64     `json:"id" db:"id"`
	UserID          int64     `json:"user_id" db:"user_id"`
	CredentialType  string    `json:"credential_type" db:"credential_type"`
	CredentialValue string    `json:"credential_value" db:"credential_value"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// Session represents an active Claude Code session
type Session struct {
	ID               int64      `json:"id" db:"id"`
	UserID           int64      `json:"user_id" db:"user_id"`
	SessionID        string     `json:"session_id" db:"session_id"`
	SlackWorkspaceID string     `json:"slack_workspace_id" db:"slack_workspace_id"`
	SlackChannelID   string     `json:"slack_channel_id" db:"slack_channel_id"`
	SlackThreadTS    string     `json:"slack_thread_ts" db:"slack_thread_ts"`
	RepoURL          string     `json:"repo_url" db:"repo_url"`
	BranchName       string     `json:"branch_name" db:"branch_name"`
	WorkTreePath     string     `json:"work_tree_path" db:"work_tree_path"`
	ClaudeProcessPID int        `json:"claude_process_pid" db:"claude_process_pid"`
	Status           string     `json:"status" db:"status"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
	EndedAt          *time.Time `json:"ended_at" db:"ended_at"`
}

// SessionMessage represents a message in a session for audit trail
type SessionMessage struct {
	ID             int64     `json:"id" db:"id"`
	SessionID      int64     `json:"session_id" db:"session_id"`
	SlackMessageTS string    `json:"slack_message_ts" db:"slack_message_ts"`
	Direction      string    `json:"direction" db:"direction"`
	Content        string    `json:"content" db:"content"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

// Request/Response types for service operations

// CreateSessionRequest represents a request to create a new session
type CreateSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
	UserID      int64  `json:"user_id"`
	ChannelID   string `json:"channel_id"`
	ThreadTS    string `json:"thread_ts"` // empty for channel-pinned sessions
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
}

// CreateUserRequest represents a request to create a new user
type CreateUserRequest struct {
	SlackWorkspaceID string `json:"slack_workspace_id"`
	SlackUserID      string `json:"slack_user_id"`
	SlackUserName    string `json:"slack_user_name"`
}

// StoreCredentialRequest represents a request to store user credentials
type StoreCredentialRequest struct {
	UserID         int64  `json:"user_id"`
	CredentialType string `json:"credential_type"`
	Value          string `json:"value"`
}

// ClaudeProcess represents a running Claude Code process
type ClaudeProcess struct {
	PID       int                 `json:"pid"`
	SessionID string              `json:"session_id"`
	StartedAt time.Time           `json:"started_at"`
	Status    string              `json:"status"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// StartCommandParams represents parsed parameters from a start command
type StartCommandParams struct {
	RepoURL   string `json:"repo_url"`
	Branch    string `json:"branch"`
	UseThread bool   `json:"use_thread"`
}

// CBError represents structured errors in the Claude Bot system
type CBError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

// Error constants
const (
	ErrCodeInvalidCommand    = "INVALID_COMMAND"
	ErrCodeSessionExists     = "SESSION_EXISTS"
	ErrCodeNoCredentials     = "NO_CREDENTIALS"
	ErrCodeClaudeUnavailable = "CLAUDE_UNAVAILABLE"
	ErrCodeRepoAccess        = "REPO_ACCESS"
	ErrCodeDatabaseError     = "DATABASE_ERROR"
	ErrCodeEncryptionError   = "ENCRYPTION_ERROR"
	ErrCodeSessionNotFound   = "SESSION_NOT_FOUND"
	ErrCodeUnauthorized      = "UNAUTHORIZED"
	ErrCodeInvalidChannel    = "INVALID_CHANNEL"
)

// NewCBError creates a new structured error
func NewCBError(code, message string, err error) *CBError {
	return &CBError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func (e *CBError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *CBError) Unwrap() error {
	return e.Err
}

// Session status constants
const (
	SessionStatusActive = "active"
	SessionStatusEnding = "ending"
	SessionStatusEnded  = "ended"
	SessionStatusError  = "error"
)

// Credential type constants
const (
	CredentialTypeAnthropic = "anthropic"
	CredentialTypeGitHub    = "github"
)

// Message direction constants
const (
	MessageDirectionUserToClaude = "user_to_claude"
	MessageDirectionClaudeToUser = "claude_to_user"
)