package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
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

// CreateSession creates a new Claude Code session (immediate response)
func (m *Manager) CreateSession(ctx context.Context, req *models.CreateSessionRequest) (*models.Session, error) {
	// Validate request
	if err := m.validateCreateSessionRequest(req); err != nil {
		return nil, err
	}

	// Check if branch name already exists
	exists, err := m.db.CheckBranchNameExists(ctx, req.FeatureName)
	if err != nil {
		return nil, fmt.Errorf("failed to check branch name: %w", err)
	}
	if exists {
		return nil, models.NewCBError(models.ErrCodeSessionExists,
			fmt.Sprintf("session with feature name '%s' already exists", req.FeatureName), nil)
	}

	// Generate session ID
	sessionID := m.generateSessionID()

	// Create session record immediately (status will be updated by background process)
	session := &models.Session{
		SessionID:        sessionID,
		SlackWorkspaceID: req.WorkspaceID,
		SlackChannelID:   req.ChannelID,
		SlackThreadTS:    req.ThreadTS,
		RepoURL:          req.RepoURL,
		BranchName:       req.FeatureName, // Use feature name as branch name
		WorkTreePath:     "",              // Will be set by background process
		RunningCost:      0.0,
		Status:           "starting", // Custom status for setup phase
	}

	// Store session in database
	if err := m.db.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	// Add the creating user as the owner of the session
	if err := m.db.AddUserToSession(ctx, session.ID, req.CreatedByUserID, models.SessionRoleOwner); err != nil {
		return nil, fmt.Errorf("failed to add owner to session: %w", err)
	}

	log.Printf("Created session %s for user %d in channel %s", sessionID, req.CreatedByUserID, req.ChannelID)
	return session, nil
}

// SetupSessionAsync sets up the repository and Claude session in the background
func (m *Manager) SetupSessionAsync(ctx context.Context, session *models.Session, req *models.CreateSessionRequest, progressCallback func(string)) {
	// This will run in a goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in session setup: %v", r)
			progressCallback(fmt.Sprintf("❌ Session setup failed: %v", r))
			m.db.UpdateSessionStatus(ctx, session.SessionID, models.SessionStatusError)
		}
	}()

	// Initialize new git manager
	gitMgr := repo.NewGoGitManager()

	// Setup repository and worktree
	result, err := gitMgr.SetupSessionRepo(ctx, req.RepoURL, req.FromCommitish, req.FeatureName, progressCallback)
	if err != nil {
		progressCallback(fmt.Sprintf("❌ Repository setup failed: %v", err))
		m.db.UpdateSessionStatus(ctx, session.SessionID, models.SessionStatusError)
		return
	}

	// Update session with worktree path
	session.WorkTreePath = result.WorktreePath
	// Note: We would need to add an UpdateSessionWorkTreePath method to update this

	// Get system prompt content
	systemPrompt, err := m.getSystemPromptContent(ctx, req)
	if err != nil {
		progressCallback(fmt.Sprintf("❌ Failed to get system prompt: %v", err))
		m.db.UpdateSessionStatus(ctx, session.SessionID, models.SessionStatusError)
		return
	}

	// Start Claude streaming session
	streamMgr := NewClaudeStreamManager()

	messageCallback := func(message string) {
		progressCallback(message)
	}

	costCallback := func(cost float64) {
		m.db.UpdateSessionCost(ctx, session.SessionID, cost)
	}

	err = streamMgr.StartStreamingSession(ctx, session.SessionID, result.WorktreePath, systemPrompt, req.ModelName, messageCallback, costCallback)
	if err != nil {
		progressCallback(fmt.Sprintf("❌ Failed to start Claude session: %v", err))
		m.db.UpdateSessionStatus(ctx, session.SessionID, models.SessionStatusError)
		return
	}

	// Mark session as active
	m.db.UpdateSessionStatus(ctx, session.SessionID, models.SessionStatusActive)
	progressCallback("✅ Session setup complete! Ready for instructions.")
}

// getSystemPromptContent retrieves the system prompt content based on the request
func (m *Manager) getSystemPromptContent(ctx context.Context, req *models.CreateSessionRequest) (string, error) {
	// If prompt text is provided, use it directly
	if req.PromptText != "" {
		return req.PromptText, nil
	}

	// If prompt name is provided, look it up
	if req.PromptName != "" {
		prompt, err := m.db.GetSystemPromptByName(ctx, req.CreatedByUserID, req.PromptName)
		if err != nil {
			return "", err
		}
		return prompt.Content, nil
	}

	// Use default prompt
	streamMgr := NewClaudeStreamManager()
	return streamMgr.GetDefaultSystemPrompt(), nil
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

	// Don't log messages for now
	// if err := m.db.CreateSessionMessage(ctx, session.ID, "", models.MessageDirectionUserToClaude, message); err != nil {
	// 	log.Printf("Failed to log incoming message: %v", err)
	// }

	// Send command to Claude process
	response, err := m.claudeMgr.SendCommand(ctx, sessionID, message)
	if err != nil {
		return "", err
	}

	// Don't log messages for now
	// if err := m.db.CreateSessionMessage(ctx, session.ID, "", models.MessageDirectionClaudeToUser, response); err != nil {
	// 	log.Printf("Failed to log outgoing message: %v", err)
	// }

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

// GetSystemPromptByName retrieves a system prompt by name for a user
func (m *Manager) GetSystemPromptByName(ctx context.Context, userID int64, name string) (*models.SystemPrompt, error) {
	return m.db.GetSystemPromptByName(ctx, userID, name)
}

// CheckBranchNameExists checks if a branch name is already in use
func (m *Manager) CheckBranchNameExists(ctx context.Context, branchName string) (bool, error) {
	return m.db.CheckBranchNameExists(ctx, branchName)
}

// GetSessionByBranchName retrieves a session by its branch name
func (m *Manager) GetSessionByBranchName(ctx context.Context, branchName string) (*models.Session, error) {
	return m.db.GetSessionByBranchName(ctx, branchName)
}

// IsUserAssociatedWithSession checks if a user is associated with a session
func (m *Manager) IsUserAssociatedWithSession(ctx context.Context, sessionID int64, userID int64) (bool, error) {
	return m.db.IsUserAssociatedWithSession(ctx, sessionID, userID)
}

// UpdateSessionThread updates the thread timestamp for a session
func (m *Manager) UpdateSessionThread(ctx context.Context, sessionID string, newThreadTS string) error {
	return m.db.UpdateSessionThread(ctx, sessionID, newThreadTS)
}

// GetSessionInfo returns detailed information about a session
func (m *Manager) GetSessionInfo(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	session, err := m.db.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	info := map[string]interface{}{
		"session_id":   session.SessionID,
		"status":       session.Status,
		"repo_url":     session.RepoURL,
		"branch":       session.BranchName,
		"running_cost": session.RunningCost,
		"created_at":   session.CreatedAt,
		"updated_at":   session.UpdatedAt,
		"channel_id":   session.SlackChannelID,
		"thread_ts":    session.SlackThreadTS,
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
	if req.FromCommitish == "" {
		return models.NewCBError(models.ErrCodeInvalidCommand, "from commitish is required", nil)
	}
	if req.FeatureName == "" {
		return models.NewCBError(models.ErrCodeInvalidCommand, "feature name is required", nil)
	}
	if req.ModelName == "" {
		return models.NewCBError(models.ErrCodeInvalidCommand, "model name is required", nil)
	}

	// Validate model name
	if req.ModelName != models.ModelSonnet && req.ModelName != models.ModelOpus {
		return models.NewCBError(models.ErrCodeInvalidCommand,
			fmt.Sprintf("invalid model '%s', must be 'sonnet' or 'opus'", req.ModelName), nil)
	}

	// Validate feature name for git branch compatibility
	if err := ValidateFeatureName(req.FeatureName); err != nil {
		return models.NewCBError(models.ErrCodeInvalidCommand, fmt.Sprintf("invalid feature name: %v", err), nil)
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

// ValidateFeatureName ensures the feature name is valid for use as a git branch name
func ValidateFeatureName(name string) error {
	if name == "" {
		return fmt.Errorf("feature name cannot be empty")
	}

	// Git branch name restrictions
	if strings.Contains(name, " ") {
		return fmt.Errorf("feature name cannot contain spaces")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("feature name cannot start or end with hyphen")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("feature name cannot contain '..'")
	}
	if strings.ContainsAny(name, "~^:?*[\\") {
		return fmt.Errorf("feature name contains invalid characters")
	}

	return nil
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

