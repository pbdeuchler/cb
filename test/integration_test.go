package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pbdeuchler/claude-bot/internal/config"
	"github.com/pbdeuchler/claude-bot/internal/db"
	"github.com/pbdeuchler/claude-bot/internal/session"
	"github.com/pbdeuchler/claude-bot/pkg/models"
)

func setupTestEnvironment(t *testing.T) (*db.DB, *session.Manager, func()) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "cb-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize test database
	database, err := db.NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	// Create test configuration
	cfg := &config.Config{
		Session: struct {
			WorkDir        string `env:"WORK_DIR" envDefault:"./sessions"`
			MaxPerUser     int    `env:"MAX_SESSIONS_PER_USER" envDefault:"5"`
			IdleTimeout    int    `env:"SESSION_IDLE_TIMEOUT" envDefault:"3600"`
			ClaudeCodePath string `env:"CLAUDE_CODE_PATH" envDefault:"claude-code"`
		}{
			WorkDir:        filepath.Join(tmpDir, "sessions"),
			MaxPerUser:     5,
			IdleTimeout:    3600,
			ClaudeCodePath: "echo", // Use echo command for testing instead of claude-code
		},
	}

	// Create session manager
	sessionMgr := session.NewManager(database, cfg)

	// Cleanup function
	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, sessionMgr, cleanup
}

func TestUserCreationAndCredentials(t *testing.T) {
	_, sessionMgr, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()

	// Test user creation
	userReq := &models.CreateUserRequest{
		SlackWorkspaceID: "T123456",
		SlackUserID:      "U123456",
		SlackUserName:    "testuser",
	}

	user, err := sessionMgr.CreateOrUpdateUser(ctx, userReq)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	if user.SlackWorkspaceID != userReq.SlackWorkspaceID {
		t.Errorf("Expected workspace ID %s, got %s", userReq.SlackWorkspaceID, user.SlackWorkspaceID)
	}

	// Test credential storage
	err = sessionMgr.StoreCredential(ctx, user.ID, models.CredentialTypeAnthropic, "test-api-key")
	if err != nil {
		t.Fatalf("Failed to store credential: %v", err)
	}

	// Test credential retrieval
	credential, err := sessionMgr.GetCredential(ctx, user.ID, models.CredentialTypeAnthropic)
	if err != nil {
		t.Fatalf("Failed to get credential: %v", err)
	}

	if credential != "test-api-key" {
		t.Errorf("Expected credential 'test-api-key', got %s", credential)
	}

	// Test required credentials check
	hasRequired, err := sessionMgr.HasRequiredCredentials(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to check required credentials: %v", err)
	}

	// Should be false because we only have anthropic, need github too
	if hasRequired {
		t.Error("Expected false for required credentials check with only anthropic credential")
	}

	// Add github credential
	err = sessionMgr.StoreCredential(ctx, user.ID, models.CredentialTypeGitHub, "test-github-token")
	if err != nil {
		t.Fatalf("Failed to store github credential: %v", err)
	}

	// Check again
	hasRequired, err = sessionMgr.HasRequiredCredentials(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to check required credentials: %v", err)
	}

	if !hasRequired {
		t.Error("Expected true for required credentials check with both credentials")
	}
}

func TestSessionLifecycle(t *testing.T) {
	// Skip this test if git is not available
	if _, err := os.Stat("/usr/bin/git"); os.IsNotExist(err) {
		if _, err := os.Stat("/bin/git"); os.IsNotExist(err) {
			t.Skip("Git not available, skipping session lifecycle test")
		}
	}

	_, sessionMgr, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()

	// Create test user
	userReq := &models.CreateUserRequest{
		SlackWorkspaceID: "T123456",
		SlackUserID:      "U123456",
		SlackUserName:    "testuser",
	}

	user, err := sessionMgr.CreateOrUpdateUser(ctx, userReq)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Store required credentials
	err = sessionMgr.StoreCredential(ctx, user.ID, models.CredentialTypeAnthropic, "test-api-key")
	if err != nil {
		t.Fatalf("Failed to store anthropic credential: %v", err)
	}

	err = sessionMgr.StoreCredential(ctx, user.ID, models.CredentialTypeGitHub, "test-github-token")
	if err != nil {
		t.Fatalf("Failed to store github credential: %v", err)
	}

	// Test session creation (this will fail because we're using echo instead of claude-code)
	// but we can test the validation and database operations
	sessionReq := &models.CreateSessionRequest{
		WorkspaceID: user.SlackWorkspaceID,
		UserID:      user.ID,
		ChannelID:   "C123456",
		ThreadTS:    "",
		RepoURL:     "https://github.com/test/repo",
		Branch:      "main",
	}

	// This will fail at the Claude process start, but that's expected in test
	_, err = sessionMgr.CreateSession(ctx, sessionReq)
	if err == nil {
		t.Error("Expected session creation to fail with echo command")
	}

	// Test getting user by Slack ID
	retrievedUser, err := sessionMgr.GetUserBySlackID(ctx, user.SlackWorkspaceID, user.SlackUserID)
	if err != nil {
		t.Fatalf("Failed to get user by Slack ID: %v", err)
	}

	if retrievedUser.ID != user.ID {
		t.Errorf("Expected user ID %d, got %d", user.ID, retrievedUser.ID)
	}

	// Test getting non-existent session
	_, err = sessionMgr.GetSession(ctx, "non-existent-session")
	if err == nil {
		t.Error("Expected error when getting non-existent session")
	}

	// Test getting active session for channel (should be none)
	activeSession, err := sessionMgr.GetActiveSessionForChannel(ctx, user.SlackWorkspaceID, "C123456", "")
	if err != nil {
		t.Fatalf("Failed to get active session for channel: %v", err)
	}

	if activeSession != nil {
		t.Error("Expected no active session for channel")
	}
}

func TestDatabaseOperations(t *testing.T) {
	database, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()

	// Test user operations
	userReq := &models.CreateUserRequest{
		SlackWorkspaceID: "T123456",
		SlackUserID:      "U123456",
		SlackUserName:    "testuser",
	}

	user1, err := database.CreateUser(ctx, userReq)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test creating same user again (should update)
	userReq.SlackUserName = "updateduser"
	user2, err := database.CreateUser(ctx, userReq)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	if user1.ID != user2.ID {
		t.Error("User ID should be the same after update")
	}

	if user2.SlackUserName != "updateduser" {
		t.Errorf("Expected updated username 'updateduser', got %s", user2.SlackUserName)
	}

	// Test credential operations
	err = database.StoreCredential(ctx, user1.ID, models.CredentialTypeAnthropic, "test-api-key")
	if err != nil {
		t.Fatalf("Failed to store credential: %v", err)
	}

	credential, err := database.GetCredential(ctx, user1.ID, models.CredentialTypeAnthropic)
	if err != nil {
		t.Fatalf("Failed to get credential: %v", err)
	}

	if credential != "test-api-key" {
		t.Errorf("Expected credential 'test-api-key', got %s", credential)
	}

	// Test getting non-existent credential
	_, err = database.GetCredential(ctx, user1.ID, models.CredentialTypeGitHub)
	if err == nil {
		t.Error("Expected error when getting non-existent credential")
	}

	// Test session operations
	session := &models.Session{
		UserID:           user1.ID,
		SessionID:        "test-session-123",
		SlackWorkspaceID: user1.SlackWorkspaceID,
		SlackChannelID:   "C123456",
		SlackThreadTS:    "",
		RepoURL:          "https://github.com/test/repo",
		BranchName:       "main",
		WorkTreePath:     "/tmp/test-session",
		ClaudeProcessPID: 12345,
		Status:           models.SessionStatusActive,
	}

	err = database.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test getting session
	retrievedSession, err := database.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if retrievedSession.SessionID != session.SessionID {
		t.Errorf("Expected session ID %s, got %s", session.SessionID, retrievedSession.SessionID)
	}

	// Test updating session status
	err = database.UpdateSessionStatus(ctx, session.SessionID, models.SessionStatusEnded)
	if err != nil {
		t.Fatalf("Failed to update session status: %v", err)
	}

	// Verify status update
	retrievedSession, err = database.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("Failed to get session after status update: %v", err)
	}

	if retrievedSession.Status != models.SessionStatusEnded {
		t.Errorf("Expected session status %s, got %s", models.SessionStatusEnded, retrievedSession.Status)
	}

	if retrievedSession.EndedAt == nil {
		t.Error("Expected EndedAt to be set when status is ended")
	}
}

func TestConcurrentOperations(t *testing.T) {
	database, sessionMgr, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()

	// Create test user
	userReq := &models.CreateUserRequest{
		SlackWorkspaceID: "T123456",
		SlackUserID:      "U123456",
		SlackUserName:    "testuser",
	}

	user, err := sessionMgr.CreateOrUpdateUser(ctx, userReq)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test concurrent credential storage
	done := make(chan bool, 2)
	errors := make(chan error, 2)

	go func() {
		err := database.StoreCredential(ctx, user.ID, models.CredentialTypeAnthropic, "value1")
		if err != nil {
			errors <- err
		}
		done <- true
	}()

	go func() {
		err := database.StoreCredential(ctx, user.ID, models.CredentialTypeGitHub, "value2")
		if err != nil {
			errors <- err
		}
		done <- true
	}()

	// Wait for both operations to complete
	timeout := time.After(5 * time.Second)
	completed := 0
	for completed < 2 {
		select {
		case <-done:
			completed++
		case err := <-errors:
			t.Fatalf("Concurrent operation failed: %v", err)
		case <-timeout:
			t.Fatal("Concurrent operations timed out")
		}
	}

	// Verify both credentials were stored
	cred1, err := database.GetCredential(ctx, user.ID, models.CredentialTypeAnthropic)
	if err != nil {
		t.Fatalf("Failed to get first credential: %v", err)
	}
	if cred1 != "value1" {
		t.Errorf("Expected credential 'value1', got %s", cred1)
	}

	cred2, err := database.GetCredential(ctx, user.ID, models.CredentialTypeGitHub)
	if err != nil {
		t.Fatalf("Failed to get second credential: %v", err)
	}
	if cred2 != "value2" {
		t.Errorf("Expected credential 'value2', got %s", cred2)
	}
}

