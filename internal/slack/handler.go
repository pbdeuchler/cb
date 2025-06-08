package slack

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/pbdeuchler/claude-bot/internal/session"
	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// EventHandler handles Slack events
type EventHandler struct {
	client        *slack.Client
	sessionMgr    *session.Manager
	parser        *CommandParser
	botUserID     string
	signingSecret string
}

// NewEventHandler creates a new Slack event handler
func NewEventHandler(client *slack.Client, sessionMgr *session.Manager, botUserID, signingSecret string) *EventHandler {
	return &EventHandler{
		client:        client,
		sessionMgr:    sessionMgr,
		parser:        NewCommandParser(botUserID),
		botUserID:     botUserID,
		signingSecret: signingSecret,
	}
}

// HandleAppMention handles app mention events
func (h *EventHandler) HandleAppMention(ctx context.Context, event *slackevents.AppMentionEvent) error {
	// Ignore messages from the bot itself
	if h.parser.IsBotMessage(event.User) {
		return nil
	}

	log.Printf("Received app mention from user %s in channel %s: %s", event.User, event.Channel, event.Text)

	// For now, use a placeholder workspace ID - in production this would come from the event context
	workspaceID := "default-workspace"

	// Get or create user
	user, err := h.getOrCreateUser(ctx, workspaceID, event.User)
	if err != nil {
		return h.sendErrorMessage(event.Channel, event.ThreadTimeStamp, "Failed to process user information", err)
	}

	// Parse command
	command, args, err := h.parser.ParseCommand(event.Text)
	if err != nil {
		return h.sendErrorMessage(event.Channel, event.ThreadTimeStamp, "", err)
	}

	// Handle command
	return h.handleCommand(ctx, user, event.Channel, event.ThreadTimeStamp, command, args)
}

// HandleMessage handles regular message events (for active sessions)
func (h *EventHandler) HandleMessage(ctx context.Context, event *slackevents.MessageEvent) error {
	// Ignore bot messages, edits, and deletes
	if h.parser.IsBotMessage(event.User) || event.SubType != "" {
		return nil
	}

	// For now, use a placeholder workspace ID - in production this would come from the event context
	workspaceID := "default-workspace"

	// Check if there's an active session in this channel/thread
	session, err := h.sessionMgr.GetActiveSessionForChannel(ctx, workspaceID, event.Channel, event.ThreadTimeStamp)
	if err != nil || session == nil {
		// No active session, ignore message
		return nil
	}

	// Forward message to Claude session
	response, err := h.sessionMgr.SendToSession(ctx, session.SessionID, event.Text)
	if err != nil {
		return h.sendErrorMessage(event.Channel, event.ThreadTimeStamp, "Failed to process message", err)
	}

	// Send response back to Slack
	return h.sendMessage(event.Channel, event.ThreadTimeStamp, response)
}

// handleCommand processes a parsed command
func (h *EventHandler) handleCommand(ctx context.Context, user *models.User, channelID, threadTS, command string, args []string) error {
	switch command {
	case "start":
		return h.handleStartCommand(ctx, user, channelID, threadTS, args)
	case "continue":
		return h.handleContinueCommand(ctx, user, channelID, threadTS, args)
	case "stop":
		return h.handleStopCommand(ctx, user, channelID, threadTS)
	case "status":
		return h.handleStatusCommand(ctx, user, channelID, threadTS)
	case "list":
		return h.handleListCommand(ctx, user, channelID, threadTS)
	case "credentials":
		return h.handleCredentialsCommand(ctx, user, channelID, threadTS, args)
	case "help":
		return h.handleHelpCommand(channelID, threadTS)
	default:
		return h.sendErrorMessage(channelID, threadTS, "",
			models.NewCBError(models.ErrCodeInvalidCommand, "Unknown command", nil))
	}
}

// handleStartCommand handles the start command
func (h *EventHandler) handleStartCommand(ctx context.Context, user *models.User, channelID, threadTS string, args []string) error {
	// Parse start command arguments using new parser
	fullCommand := fmt.Sprintf("@%s start %s", h.botUserID, strings.Join(args, " "))
	cmdArgs, err := ParseStartCommandNew(fullCommand)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "", err)
	}

	// Check if user has required credentials
	hasCredentials, err := h.sessionMgr.HasRequiredCredentials(ctx, user.ID)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to check credentials", err)
	}
	if !hasCredentials {
		return h.sendErrorMessage(channelID, threadTS, "",
			models.NewCBError(models.ErrCodeNoCredentials,
				"Missing required credentials. Use `credentials set {github|anthropic} <secret>` to continue", nil))
	}

	// Create a new thread for this session
	initialMsg := fmt.Sprintf("🚀 Starting session '%s' with model %s...", cmdArgs.Feature, cmdArgs.Model)

	// Send initial message and get thread timestamp
	_, sessionThreadTS, err := h.client.PostMessage(channelID, slack.MsgOptionText(initialMsg, false))
	if err != nil {
		return fmt.Errorf("failed to create session thread: %w", err)
	}

	// Create session request
	req := &models.CreateSessionRequest{
		WorkspaceID:     user.SlackWorkspaceID,
		CreatedByUserID: user.ID,
		ChannelID:       channelID,
		ThreadTS:        sessionThreadTS,
		RepoURL:         cmdArgs.RepoURL,
		FromCommitish:   cmdArgs.From,
		FeatureName:     cmdArgs.Feature,
		ModelName:       cmdArgs.Model,
		PromptText:      cmdArgs.Prompt,
		PromptName:      cmdArgs.PName,
	}

	// Create session (immediate response)
	session, err := h.sessionMgr.CreateSession(ctx, req)
	if err != nil {
		return h.sendErrorMessage(channelID, sessionThreadTS, "Failed to start session", err)
	}

	// Send success message
	successMsg := fmt.Sprintf("✅ Session '%s' created!\n\nSetup is now running in the background...", session.BranchName)
	h.sendMessage(channelID, sessionThreadTS, successMsg)

	// Start background setup
	go func() {
		progressCallback := func(message string) {
			h.sendMessage(channelID, sessionThreadTS, message)
		}
		h.sessionMgr.SetupSessionAsync(context.Background(), session, req, progressCallback)
	}()

	return nil
}

// handleContinueCommand handles the continue command
func (h *EventHandler) handleContinueCommand(ctx context.Context, user *models.User, channelID, threadTS string, args []string) error {
	// Parse continue command arguments
	fullCommand := fmt.Sprintf("@%s continue %s", h.botUserID, strings.Join(args, " "))
	cmdArgs, err := ParseContinueCommand(fullCommand)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "", err)
	}

	// Find session by branch name
	session, err := h.sessionMgr.GetSessionByBranchName(ctx, cmdArgs.Feature)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to find session", err)
	}

	// Check if user is associated with this session
	isAssociated, err := h.sessionMgr.IsUserAssociatedWithSession(ctx, session.ID, user.ID)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to check session access", err)
	}
	if !isAssociated {
		return h.sendErrorMessage(channelID, threadTS, "",
			models.NewCBError(models.ErrCodeUnauthorized,
				fmt.Sprintf("You are not associated with session '%s'", cmdArgs.Feature), nil))
	}

	// Create a new thread for this session
	initialMsg := fmt.Sprintf("🔄 Continuing session '%s' in this thread...", cmdArgs.Feature)

	err = h.sendMessage(channelID, threadTS, initialMsg)
	if err != nil {
		return fmt.Errorf("failed to create new session thread: %w", err)
	}

	// Store the old thread info for notification
	oldChannelID := session.SlackChannelID
	oldThreadTS := session.SlackThreadTS

	// Update the session thread
	err = h.sessionMgr.UpdateSessionThread(ctx, session.SessionID, threadTS)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to update session thread", err)
	}

	// Send success message in new thread
	successMsg := fmt.Sprintf("✅ Session '%s' has been moved to this thread!\n\n"+
		"📊 **Session Info:**\n"+
		"• Repository: %s\n"+
		"• Branch: %s\n"+
		"• Status: %s\n"+
		"• Running Cost: $%.4f",
		session.BranchName, session.RepoURL, session.BranchName, session.Status, session.RunningCost)

	h.sendMessage(channelID, threadTS, successMsg)

	// Send notification to old thread (if different from current location)
	if oldChannelID != "" && oldThreadTS != "" && (oldChannelID != channelID || oldThreadTS != threadTS) {
		oldThreadMsg := fmt.Sprintf("📍 Session '%s' has been moved to a new thread.\n\n"+
			"Messages will no longer be monitored in this thread.", cmdArgs.Feature)
		h.sendMessage(oldChannelID, oldThreadTS, oldThreadMsg)
	}

	return nil
}

// handleStopCommand handles the stop command
func (h *EventHandler) handleStopCommand(ctx context.Context, user *models.User, channelID, threadTS string) error {
	// Find active session in this channel/thread
	session, err := h.sessionMgr.GetActiveSessionForChannel(ctx, user.SlackWorkspaceID, channelID, threadTS)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to find session", err)
	}
	if session == nil {
		return h.sendErrorMessage(channelID, threadTS, "",
			models.NewCBError(models.ErrCodeSessionNotFound, "No active session in this channel/thread", nil))
	}

	// Check if user owns the session
	ownerID, err := h.sessionMgr.GetSessionOwner(ctx, session.ID)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to get session owner", err)
	}
	if ownerID != user.ID {
		return h.sendErrorMessage(channelID, threadTS, "",
			models.NewCBError(models.ErrCodeUnauthorized, "You can only stop your own sessions", nil))
	}

	// End session
	if err := h.sessionMgr.EndSession(ctx, session.SessionID); err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to stop session", err)
	}

	return h.sendMessage(channelID, threadTS, FormatSuccessMessage("Session stopped and changes committed"))
}

// handleStatusCommand handles the status command
func (h *EventHandler) handleStatusCommand(ctx context.Context, user *models.User, channelID, threadTS string) error {
	// Find active session in this channel/thread
	session, err := h.sessionMgr.GetActiveSessionForChannel(ctx, user.SlackWorkspaceID, channelID, threadTS)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to find session", err)
	}
	if session == nil {
		return h.sendMessage(channelID, threadTS, "No active session in this channel/thread")
	}

	// Get detailed session info
	info, err := h.sessionMgr.GetSessionInfo(ctx, session.SessionID)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to get session info", err)
	}

	return h.sendMessage(channelID, threadTS, FormatSessionInfo(info))
}

// handleListCommand handles the list command
func (h *EventHandler) handleListCommand(ctx context.Context, user *models.User, channelID, threadTS string) error {
	sessions, err := h.sessionMgr.GetUserSessions(ctx, user.ID)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "Failed to get sessions", err)
	}

	if len(sessions) == 0 {
		return h.sendMessage(channelID, threadTS, "You have no active sessions")
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("*Your Active Sessions (%d):*", len(sessions)))

	for _, session := range sessions {
		info := map[string]any{
			"session_id": session.SessionID,
			"status":     session.Status,
			"repo_url":   session.RepoURL,
			"branch":     session.BranchName,
		}
		parts = append(parts, fmt.Sprintf("\n• Channel: <#%s>", session.SlackChannelID))
		parts = append(parts, FormatSessionInfo(info))
	}

	return h.sendMessage(channelID, threadTS, strings.Join(parts, "\n"))
}

// handleCredentialsCommand handles credential-related commands
func (h *EventHandler) handleCredentialsCommand(ctx context.Context, user *models.User, channelID, threadTS string, args []string) error {
	action, credType, value, err := ParseCredentialCommand(args)
	if err != nil {
		return h.sendErrorMessage(channelID, threadTS, "", err)
	}

	switch action {
	case "set":
		if err := h.sessionMgr.StoreCredential(ctx, user.ID, credType, value); err != nil {
			return h.sendErrorMessage(channelID, threadTS, "Failed to store credential", err)
		}
		return h.sendMessage(channelID, threadTS, FormatSuccessMessage(fmt.Sprintf("%s credential stored securely", credType)))

	case "list":
		// Get stored credential types (without values for security)
		hasAnthropic := false
		hasGithub := false

		if _, err := h.sessionMgr.GetCredential(ctx, user.ID, models.CredentialTypeAnthropic); err == nil {
			hasAnthropic = true
		}
		if _, err := h.sessionMgr.GetCredential(ctx, user.ID, models.CredentialTypeGitHub); err == nil {
			hasGithub = true
		}

		var parts []string
		parts = append(parts, "*Your Stored Credentials:*")

		if hasAnthropic {
			parts = append(parts, "• :white_check_mark: Anthropic API key")
		} else {
			parts = append(parts, "• :x: Anthropic API key (required)")
		}

		if hasGithub {
			parts = append(parts, "• :white_check_mark: GitHub token")
		} else {
			parts = append(parts, "• :x: GitHub token (optional)")
		}

		return h.sendMessage(channelID, threadTS, strings.Join(parts, "\n"))
	}

	return nil
}

// handleHelpCommand handles the help command
func (h *EventHandler) handleHelpCommand(channelID, threadTS string) error {
	return h.sendMessage(channelID, threadTS, FormatHelpMessage())
}

// getOrCreateUser gets or creates a user record
func (h *EventHandler) getOrCreateUser(ctx context.Context, workspaceID, userID string) (*models.User, error) {
	// Try to get existing user
	user, err := h.sessionMgr.GetUserBySlackID(ctx, workspaceID, userID)
	if user != nil && err == nil {
		return user, nil
	} else if err != nil {
		return nil, err
	}

	// User doesn't exist, get user info from Slack
	userInfo, err := h.client.GetUserInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info from Slack: %w", err)
	}

	// Create user
	req := &models.CreateUserRequest{
		SlackWorkspaceID: workspaceID,
		SlackUserID:      userID,
		SlackUserName:    userInfo.Name,
	}

	return h.sessionMgr.CreateOrUpdateUser(ctx, req)
}

// sendMessage sends a message to Slack
func (h *EventHandler) sendMessage(channelID, threadTS, text string) error {
	options := []slack.MsgOption{
		slack.MsgOptionText(text, false),
		slack.MsgOptionAsUser(true),
	}

	if threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	_, _, err := h.client.PostMessage(channelID, options...)
	if err != nil {
		log.Printf("Failed to send message to Slack: %v", err)
	}
	return err
}

// sendErrorMessage sends an error message to Slack
func (h *EventHandler) sendErrorMessage(channelID, threadTS, context string, err error) error {
	message := FormatErrorMessage(err)
	if context != "" {
		message = fmt.Sprintf("%s: %s", context, message)
	}

	return h.sendMessage(channelID, threadTS, message)
}

// sendEphemeralMessage sends an ephemeral message to a user
func (h *EventHandler) sendEphemeralMessage(channelID, userID, text string) error {
	_, err := h.client.PostEphemeral(channelID, userID, slack.MsgOptionText(text, false))
	if err != nil {
		log.Printf("Failed to send ephemeral message to Slack: %v", err)
	}
	return err
}
