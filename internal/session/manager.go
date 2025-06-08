package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/pbdeuchler/claude-bot/internal/config"
	"github.com/pbdeuchler/claude-bot/internal/db"
	"github.com/pbdeuchler/claude-bot/internal/repo"
	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// Manager manages Claude Code sessions
type Manager struct {
	db        *db.DB
	claudeMgr *ClaudeManager
	repoMgr   *repo.GitManager
	config    *config.Config
	mu        sync.RWMutex
}

// NewManager creates a new session manager
func NewManager(database *db.DB, cfg *config.Config) *Manager {
	return &Manager{
		db:        database,
		claudeMgr: NewClaudeManager(cfg.Session.ClaudeCodePath),
		repoMgr:   repo.NewGitManager(),
		config:    cfg,
	}
}

// CreateSession creates a new Claude Code session
func (m *Manager) CreateSession(ctx context.Context, req *models.CreateSessionRequest) (*models.Session, error) {
	// Validate request
	if err := m.validateCreateSessionRequest(req); err != nil {
		return nil, err
	}

	// Check for existing active session in this channel/thread
	existing, err := m.db.GetActiveSessionForChannel(ctx, req.WorkspaceID, req.ChannelID, req.ThreadTS)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing session: %w", err)
	}
	if existing != nil {
		return nil, models.NewCBError(models.ErrCodeSessionExists, "active session already exists in this context", nil)
	}

	// Check user session limit
	activeSessions, err := m.db.GetActiveSessionsByUser(ctx, req.CreatedByUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to check user session count: %w", err)
	}
	if len(activeSessions) >= m.config.Session.MaxPerUser {
		return nil, models.NewCBError(models.ErrCodeSessionExists, 
			fmt.Sprintf("user has reached maximum session limit (%d)", m.config.Session.MaxPerUser), nil)
	}

	// Validate repository access
	if err := m.repoMgr.ValidateRepoURL(ctx, req.RepoURL); err != nil {
		return nil, err
	}

	// Generate session ID and work tree path
	sessionID := m.generateSessionID()
	workTreePath := filepath.Join(m.config.Session.WorkDir, sessionID)

	// Clone or create work tree
	if err := m.repoMgr.CloneOrCreateWorkTree(ctx, req.RepoURL, req.Branch, workTreePath); err != nil {
		return nil, fmt.Errorf("failed to setup repository: %w", err)
	}

	// Get user credentials
	apiKey, err := m.db.GetCredential(ctx, req.CreatedByUserID, models.CredentialTypeAnthropic)
	if err != nil {
		// Cleanup work tree on failure
		m.repoMgr.Cleanup(ctx, workTreePath)
		return nil, err
	}

	// Start Claude Code session
	_, err = m.claudeMgr.StartSession(ctx, sessionID, workTreePath, apiKey)
	if err != nil {
		// Cleanup work tree on failure
		m.repoMgr.Cleanup(ctx, workTreePath)
		return nil, fmt.Errorf("failed to start Claude session: %w", err)
	}

	// Create session record
	session := &models.Session{
		SessionID:        sessionID,
		SlackWorkspaceID: req.WorkspaceID,
		SlackChannelID:   req.ChannelID,
		SlackThreadTS:    req.ThreadTS,
		RepoURL:          req.RepoURL,
		BranchName:       req.Branch,
		WorkTreePath:     workTreePath,
		RunningCost:      0.0,
		Status:           models.SessionStatusActive,
	}

	// Store session in database
	if err := m.db.CreateSession(ctx, session); err != nil {
		// Cleanup on failure
		m.claudeMgr.StopSession(ctx, sessionID)
		m.repoMgr.Cleanup(ctx, workTreePath)
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	// Add the creating user as the owner of the session
	if err := m.db.AddUserToSession(ctx, session.ID, req.CreatedByUserID, models.SessionRoleOwner); err != nil {
		// Cleanup on failure
		m.claudeMgr.StopSession(ctx, sessionID)
		m.repoMgr.Cleanup(ctx, workTreePath)
		return nil, fmt.Errorf("failed to add owner to session: %w", err)
	}

	log.Printf("Created session %s for user %d in channel %s", sessionID, req.CreatedByUserID, req.ChannelID)
	return session, nil
}

// GetSession retrieves a session by ID
func (m *Manager) GetSession(ctx context.Context, sessionID string) (*models.Session, error) {
	return m.db.GetSession(ctx, sessionID)
}

// GetActiveSessionForChannel retrieves an active session for a specific channel/thread
func (m *Manager) GetActiveSessionForChannel(ctx context.Context, workspaceID, channelID, threadTS string) (*models.Session, error) {
	return m.db.GetActiveSessionForChannel(ctx, workspaceID, channelID, threadTS)
}

// SendToSession sends a command to a Claude session
func (m *Manager) SendToSession(ctx context.Context, sessionID, message string) (string, error) {
	// Get session from database
	session, err := m.db.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	if session.Status != models.SessionStatusActive {
		return "", models.NewCBError(models.ErrCodeClaudeUnavailable, "session is not active", nil)
	}

	// Log incoming message
	if err := m.db.CreateSessionMessage(ctx, session.ID, "", models.MessageDirectionUserToClaude, message); err != nil {
		log.Printf("Failed to log incoming message: %v", err)
	}

	// Send command to Claude process
	response, err := m.claudeMgr.SendCommand(ctx, sessionID, message)
	if err != nil {
		return "", err
	}

	// Log outgoing response
	if err := m.db.CreateSessionMessage(ctx, session.ID, "", models.MessageDirectionClaudeToUser, response); err != nil {
		log.Printf("Failed to log outgoing message: %v", err)
	}

	return response, nil
}

// EndSession gracefully ends a Claude session
func (m *Manager) EndSession(ctx context.Context, sessionID string) error {
	session, err := m.db.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.Status != models.SessionStatusActive {
		return models.NewCBError(models.ErrCodeSessionNotFound, "session is not active", nil)
	}

	log.Printf("Ending session %s", sessionID)

	// Update status to ending
	if err := m.db.UpdateSessionStatus(ctx, sessionID, models.SessionStatusEnding); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Stop Claude process
	if err := m.claudeMgr.StopSession(ctx, sessionID); err != nil {
		log.Printf("Failed to stop Claude process for session %s: %v", sessionID, err)
	}

	// Commit and push changes
	commitMsg := fmt.Sprintf("CB Session %s changes", sessionID)
	if err := m.repoMgr.CommitAndPush(ctx, session.WorkTreePath, session.BranchName, commitMsg); err != nil {
		log.Printf("Failed to commit changes for session %s: %v", sessionID, err)
	}

	// Cleanup work tree
	if err := m.repoMgr.Cleanup(ctx, session.WorkTreePath); err != nil {
		log.Printf("Failed to cleanup work tree for session %s: %v", sessionID, err)
	}

	// Update status to ended
	if err := m.db.UpdateSessionStatus(ctx, sessionID, models.SessionStatusEnded); err != nil {
		return fmt.Errorf("failed to mark session as ended: %w", err)
	}

	log.Printf("Session %s ended successfully", sessionID)
	return nil
}

// EndAllActiveSessions ends all active sessions (used during shutdown)
func (m *Manager) EndAllActiveSessions(ctx context.Context) error {
	sessions, err := m.db.GetAllActiveSessions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active sessions: %w", err)
	}

	var errors []error
	for _, session := range sessions {
		if err := m.EndSession(ctx, session.SessionID); err != nil {
			errors = append(errors, fmt.Errorf("failed to end session %s: %w", session.SessionID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors ending sessions: %v", errors)
	}

	return nil
}

// GetUserSessions returns all sessions for a user
func (m *Manager) GetUserSessions(ctx context.Context, userID int64) ([]*models.Session, error) {
	return m.db.GetActiveSessionsByUser(ctx, userID)
}

// StoreCredential stores user credentials
func (m *Manager) StoreCredential(ctx context.Context, userID int64, credType, value string) error {
	return m.db.StoreCredential(ctx, userID, credType, value)
}

// GetCredential retrieves user credentials
func (m *Manager) GetCredential(ctx context.Context, userID int64, credType string) (string, error) {
	return m.db.GetCredential(ctx, userID, credType)
}

// HasRequiredCredentials checks if user has all required credentials
func (m *Manager) HasRequiredCredentials(ctx context.Context, userID int64) (bool, error) {
	return m.db.HasRequiredCredentials(ctx, userID)
}

// CreateOrUpdateUser creates or updates a user
func (m *Manager) CreateOrUpdateUser(ctx context.Context, req *models.CreateUserRequest) (*models.User, error) {
	return m.db.CreateUser(ctx, req)
}

// GetUserBySlackID retrieves a user by Slack workspace and user ID
func (m *Manager) GetUserBySlackID(ctx context.Context, workspaceID, userID string) (*models.User, error) {
	return m.db.GetUserBySlackID(ctx, workspaceID, userID)
}

// GetSessionOwner retrieves the owner user ID for a session
func (m *Manager) GetSessionOwner(ctx context.Context, sessionID int64) (int64, error) {
	return m.db.GetSessionOwner(ctx, sessionID)
}

// UpdateSessionCost updates the running cost for a session
func (m *Manager) UpdateSessionCost(ctx context.Context, sessionID string, cost float64) error {
	return m.db.UpdateSessionCost(ctx, sessionID, cost)
}

// GetSessionInfo returns detailed information about a session
func (m *Manager) GetSessionInfo(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	session, err := m.db.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	info := map[string]interface{}{
		"session_id":    session.SessionID,
		"status":        session.Status,
		"repo_url":      session.RepoURL,
		"branch":        session.BranchName,
		"running_cost":  session.RunningCost,
		"created_at":    session.CreatedAt,
		"updated_at":    session.UpdatedAt,
		"channel_id":    session.SlackChannelID,
		"thread_ts":     session.SlackThreadTS,
	}

	// Get Claude process status
	if claudeProcess, err := m.claudeMgr.GetSession(sessionID); err == nil {
		info["claude_status"] = claudeProcess.GetStatus()
		info["claude_started_at"] = claudeProcess.StartedAt
	}

	// Get repository info
	if repoInfo, err := m.repoMgr.GetRepoInfo(ctx, session.WorkTreePath); err == nil {
		info["repo_info"] = repoInfo
	}

	return info, nil
}

// Private helper methods

func (m *Manager) validateCreateSessionRequest(req *models.CreateSessionRequest) error {
	if req.WorkspaceID == "" {
		return models.NewCBError(models.ErrCodeInvalidCommand, "workspace ID is required", nil)
	}
	if req.CreatedByUserID == 0 {
		return models.NewCBError(models.ErrCodeInvalidCommand, "user ID is required", nil)
	}
	if req.ChannelID == "" {
		return models.NewCBError(models.ErrCodeInvalidCommand, "channel ID is required", nil)
	}
	if req.RepoURL == "" {
		return models.NewCBError(models.ErrCodeInvalidCommand, "repository URL is required", nil)
	}
	if req.Branch == "" {
		req.Branch = "main" // Default to main branch
	}

	// Check channel restrictions
	if req.ChannelID == "general" {
		return models.NewCBError(models.ErrCodeInvalidChannel, "sessions cannot be started in #general", nil)
	}

	return nil
}

func (m *Manager) generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// StartIdleSessionMonitor starts a goroutine to monitor and cleanup idle sessions
func (m *Manager) StartIdleSessionMonitor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanupIdleSessions(ctx)
		}
	}
}

func (m *Manager) cleanupIdleSessions(ctx context.Context) {
	sessions, err := m.db.GetAllActiveSessions(ctx)
	if err != nil {
		log.Printf("Failed to get active sessions for cleanup: %v", err)
		return
	}

	idleTimeout := time.Duration(m.config.Session.IdleTimeout) * time.Second
	now := time.Now()

	for _, session := range sessions {
		if now.Sub(session.UpdatedAt) > idleTimeout {
			log.Printf("Cleaning up idle session %s", session.SessionID)
			if err := m.EndSession(ctx, session.SessionID); err != nil {
				log.Printf("Failed to cleanup idle session %s: %v", session.SessionID, err)
			}
		}
	}
}