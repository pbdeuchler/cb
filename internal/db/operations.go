package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// User operations

func (db *DB) CreateUser(ctx context.Context, req *models.CreateUserRequest) (*models.User, error) {
	query := `
		INSERT INTO users (slack_workspace_id, slack_user_id, slack_user_name)
		VALUES (?, ?, ?)
		ON CONFLICT(slack_workspace_id, slack_user_id) 
		DO UPDATE SET 
			slack_user_name = excluded.slack_user_name,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, slack_workspace_id, slack_user_id, slack_user_name, created_at, updated_at
	`

	var user models.User
	err := db.conn.QueryRowContext(ctx, query, req.SlackWorkspaceID, req.SlackUserID, req.SlackUserName).Scan(
		&user.ID, &user.SlackWorkspaceID, &user.SlackUserID, &user.SlackUserName, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

func (db *DB) GetUserBySlackID(ctx context.Context, workspaceID, userID string) (*models.User, error) {
	query := `
		SELECT id, slack_workspace_id, slack_user_id, slack_user_name, created_at, updated_at
		FROM users 
		WHERE slack_workspace_id = ? AND slack_user_id = ?
	`

	var user models.User
	err := db.conn.QueryRowContext(ctx, query, workspaceID, userID).Scan(
		&user.ID, &user.SlackWorkspaceID, &user.SlackUserID, &user.SlackUserName, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// User not found, return nil
			// TODO: consider a better return scheme here
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// Credential operations

func (db *DB) StoreCredential(ctx context.Context, userID int64, credType, value string) error {
	// First try to update existing credential
	updateQuery := `
		UPDATE credentials 
		SET credential_value = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND credential_type = ?
	`

	result, err := db.conn.ExecContext(ctx, updateQuery, value, userID, credType)
	if err != nil {
		return fmt.Errorf("failed to update credential: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// If no rows were updated, insert new credential
	if rowsAffected == 0 {
		insertQuery := `
			INSERT INTO credentials (user_id, credential_type, credential_value)
			VALUES (?, ?, ?)
		`

		_, err = db.conn.ExecContext(ctx, insertQuery, userID, credType, value)
		if err != nil {
			return fmt.Errorf("failed to insert credential: %w", err)
		}
	}

	return nil
}

func (db *DB) GetCredential(ctx context.Context, userID int64, credType string) (string, error) {
	query := `
		SELECT credential_value 
		FROM credentials 
		WHERE user_id = ? AND credential_type = ?
	`

	var value string
	err := db.conn.QueryRowContext(ctx, query, userID, credType).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", models.NewCBError(models.ErrCodeNoCredentials, "credential not found", err)
		}
		return "", fmt.Errorf("failed to get credential: %w", err)
	}

	return value, nil
}

func (db *DB) HasRequiredCredentials(ctx context.Context, userID int64) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM credentials 
		WHERE user_id = ? AND credential_type IN ('anthropic', 'github')
	`

	var count int
	err := db.conn.QueryRowContext(ctx, query, userID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check credentials: %w", err)
	}

	return count >= 2, nil
}

// Session operations

func (db *DB) CreateSession(ctx context.Context, session *models.Session) error {
	query := `
		INSERT INTO sessions (
			session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			repo_url, branch_name, work_tree_path, model_name, running_cost, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	err := db.conn.QueryRowContext(ctx, query,
		session.SessionID, session.SlackWorkspaceID, session.SlackChannelID,
		session.SlackThreadTS, session.RepoURL, session.BranchName, session.WorkTreePath,
		session.ModelName, session.RunningCost, session.Status,
	).Scan(&session.ID)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

func (db *DB) GetSession(ctx context.Context, sessionID string) (*models.Session, error) {
	query := `
		SELECT id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, model_name, running_cost, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE session_id = ?
	`

	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID, &session.SessionID, &session.SlackWorkspaceID,
		&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
		&session.WorkTreePath, &session.ModelName, &session.RunningCost, &session.Status,
		&session.CreatedAt, &session.UpdatedAt, &session.EndedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.NewCBError(models.ErrCodeSessionNotFound, "session not found", err)
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

func (db *DB) GetActiveSessionForChannel(ctx context.Context, workspaceID, channelID, threadTS string) (*models.Session, error) {
	query := `
		SELECT id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, model_name, running_cost, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE slack_workspace_id = ? AND slack_channel_id = ? AND slack_thread_ts = ? AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1
	`

	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, workspaceID, channelID, threadTS).Scan(
		&session.ID, &session.SessionID, &session.SlackWorkspaceID,
		&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
		&session.WorkTreePath, &session.ModelName, &session.RunningCost, &session.Status,
		&session.CreatedAt, &session.UpdatedAt, &session.EndedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No active session found, not an error
		}
		return nil, fmt.Errorf("failed to get active session: %w", err)
	}

	return &session, nil
}

func (db *DB) GetActiveSessionsByUser(ctx context.Context, userID int64) ([]*models.Session, error) {
	query := `
		SELECT DISTINCT s.id, s.session_id, s.slack_workspace_id, s.slack_channel_id, s.slack_thread_ts,
			   s.repo_url, s.branch_name, s.work_tree_path, s.model_name, s.running_cost, s.status,
			   s.created_at, s.updated_at, s.ended_at
		FROM sessions s
		INNER JOIN session_users su ON s.id = su.session_id
		WHERE su.user_id = ? AND s.status = 'active'
		ORDER BY s.created_at DESC
	`

	rows, err := db.conn.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		var session models.Session
		err := rows.Scan(
			&session.ID, &session.SessionID, &session.SlackWorkspaceID,
			&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
			&session.WorkTreePath, &session.ModelName, &session.RunningCost, &session.Status,
			&session.CreatedAt, &session.UpdatedAt, &session.EndedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

func (db *DB) UpdateSessionStatus(ctx context.Context, sessionID, status string) error {
	query := `
		UPDATE sessions 
		SET status = ?, updated_at = CURRENT_TIMESTAMP, ended_at = CASE WHEN ? = 'ended' THEN CURRENT_TIMESTAMP ELSE ended_at END
		WHERE session_id = ?
	`

	result, err := db.conn.ExecContext(ctx, query, status, status, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "session not found", nil)
	}

	return nil
}

func (db *DB) UpdateSessionCost(ctx context.Context, sessionID string, cost float64) error {
	query := `
		UPDATE sessions 
		SET running_cost = ?, updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ?
	`

	_, err := db.conn.ExecContext(ctx, query, cost, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session cost: %w", err)
	}

	return nil
}

func (db *DB) UpdateSessionThread(ctx context.Context, sessionID string, newThreadTS string) error {
	query := `
		UPDATE sessions 
		SET slack_thread_ts = ?, updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ?
	`

	result, err := db.conn.ExecContext(ctx, query, newThreadTS, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session thread: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "session not found", nil)
	}

	return nil
}

func (db *DB) UpdateSessionByID(ctx context.Context, sessionDBID int64, sessionID string) error {
	query := `
		UPDATE sessions 
		SET session_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	result, err := db.conn.ExecContext(ctx, query, sessionID, sessionDBID)
	if err != nil {
		return fmt.Errorf("failed to update session ID: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "session not found", nil)
	}

	return nil
}

func (db *DB) UpdateSessionStatusByID(ctx context.Context, sessionDBID int64, status string) error {
	query := `
		UPDATE sessions 
		SET status = ?, updated_at = CURRENT_TIMESTAMP, ended_at = CASE WHEN ? = 'ended' THEN CURRENT_TIMESTAMP ELSE ended_at END
		WHERE id = ?
	`

	result, err := db.conn.ExecContext(ctx, query, status, status, sessionDBID)
	if err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "session not found", nil)
	}

	return nil
}

func (db *DB) UpdateSessionCostByID(ctx context.Context, sessionDBID int64, cost float64) error {
	query := `
		UPDATE sessions 
		SET running_cost = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	result, err := db.conn.ExecContext(ctx, query, cost, sessionDBID)
	if err != nil {
		return fmt.Errorf("failed to update session cost: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "session not found", nil)
	}

	return nil
}

func (db *DB) GetAllActiveSessions(ctx context.Context) ([]*models.Session, error) {
	query := `
		SELECT id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, model_name, running_cost, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE status = 'active'
		ORDER BY created_at DESC
	`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		var session models.Session
		err := rows.Scan(
			&session.ID, &session.SessionID, &session.SlackWorkspaceID,
			&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
			&session.WorkTreePath, &session.ModelName, &session.RunningCost, &session.Status,
			&session.CreatedAt, &session.UpdatedAt, &session.EndedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

// Session message operations

func (db *DB) CreateSessionMessage(ctx context.Context, sessionID int64, messageTS, direction, content string) error {
	query := `
		INSERT INTO session_messages (session_id, slack_message_ts, direction, content)
		VALUES (?, ?, ?, ?)
	`

	_, err := db.conn.ExecContext(ctx, query, sessionID, messageTS, direction, content)
	if err != nil {
		return fmt.Errorf("failed to create session message: %w", err)
	}

	return nil
}

func (db *DB) GetSessionMessages(ctx context.Context, sessionID int64, limit int) ([]*models.SessionMessage, error) {
	query := `
		SELECT id, session_id, slack_message_ts, direction, content, created_at
		FROM session_messages 
		WHERE session_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := db.conn.QueryContext(ctx, query, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get session messages: %w", err)
	}
	defer rows.Close()

	var messages []*models.SessionMessage
	for rows.Next() {
		var message models.SessionMessage
		err := rows.Scan(
			&message.ID, &message.SessionID, &message.SlackMessageTS,
			&message.Direction, &message.Content, &message.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session message: %w", err)
		}
		messages = append(messages, &message)
	}

	return messages, nil
}

// System prompt operations

func (db *DB) CreateSystemPrompt(ctx context.Context, req *models.CreateSystemPromptRequest) (*models.SystemPrompt, error) {
	query := `
		INSERT INTO system_prompts (name, description, content, is_public, created_by)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, name, description, content, is_public, created_by, created_at, updated_at
	`

	var prompt models.SystemPrompt
	err := db.conn.QueryRowContext(ctx, query, req.Name, req.Description, req.Content, req.IsPublic, req.CreatedBy).Scan(
		&prompt.ID, &prompt.Name, &prompt.Description, &prompt.Content, &prompt.IsPublic, &prompt.CreatedBy, &prompt.CreatedAt, &prompt.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create system prompt: %w", err)
	}

	return &prompt, nil
}

func (db *DB) GetSystemPrompt(ctx context.Context, id int64) (*models.SystemPrompt, error) {
	query := `
		SELECT id, name, description, content, is_public, created_by, created_at, updated_at
		FROM system_prompts 
		WHERE id = ?
	`

	var prompt models.SystemPrompt
	err := db.conn.QueryRowContext(ctx, query, id).Scan(
		&prompt.ID, &prompt.Name, &prompt.Description, &prompt.Content, &prompt.IsPublic, &prompt.CreatedBy, &prompt.CreatedAt, &prompt.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.NewCBError(models.ErrCodeSessionNotFound, "system prompt not found", err)
		}
		return nil, fmt.Errorf("failed to get system prompt: %w", err)
	}

	return &prompt, nil
}

func (db *DB) GetSystemPromptsByUser(ctx context.Context, userID int64) ([]*models.SystemPrompt, error) {
	query := `
		SELECT DISTINCT sp.id, sp.name, sp.description, sp.content, sp.is_public, sp.created_by, sp.created_at, sp.updated_at
		FROM system_prompts sp
		LEFT JOIN user_system_prompts usp ON sp.id = usp.system_prompt_id
		WHERE sp.created_by = ? OR usp.user_id = ? OR sp.is_public = TRUE
		ORDER BY sp.created_at DESC
	`

	rows, err := db.conn.QueryContext(ctx, query, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get system prompts: %w", err)
	}
	defer rows.Close()

	var prompts []*models.SystemPrompt
	for rows.Next() {
		var prompt models.SystemPrompt
		err := rows.Scan(
			&prompt.ID, &prompt.Name, &prompt.Description, &prompt.Content, &prompt.IsPublic, &prompt.CreatedBy, &prompt.CreatedAt, &prompt.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan system prompt: %w", err)
		}
		prompts = append(prompts, &prompt)
	}

	return prompts, nil
}

func (db *DB) GetSystemPromptByName(ctx context.Context, userID int64, name string) (*models.SystemPrompt, error) {
	query := `
		SELECT DISTINCT sp.id, sp.name, sp.description, sp.content, sp.is_public, sp.created_by, sp.created_at, sp.updated_at
		FROM system_prompts sp
		LEFT JOIN user_system_prompts usp ON sp.id = usp.system_prompt_id
		WHERE (sp.created_by = ? OR usp.user_id = ? OR sp.is_public = TRUE) AND sp.name = ?
		LIMIT 1
	`

	var prompt models.SystemPrompt
	err := db.conn.QueryRowContext(ctx, query, userID, userID, name).Scan(
		&prompt.ID, &prompt.Name, &prompt.Description, &prompt.Content, &prompt.IsPublic, &prompt.CreatedBy, &prompt.CreatedAt, &prompt.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.NewCBError(models.ErrCodeSessionNotFound, "system prompt not found", err)
		}
		return nil, fmt.Errorf("failed to get system prompt by name: %w", err)
	}

	return &prompt, nil
}

func (db *DB) UpdateSystemPrompt(ctx context.Context, req *models.UpdateSystemPromptRequest) (*models.SystemPrompt, error) {
	query := `
		UPDATE system_prompts 
		SET name = ?, description = ?, content = ?, is_public = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING id, name, description, content, is_public, created_by, created_at, updated_at
	`

	var prompt models.SystemPrompt
	err := db.conn.QueryRowContext(ctx, query, req.Name, req.Description, req.Content, req.IsPublic, req.ID).Scan(
		&prompt.ID, &prompt.Name, &prompt.Description, &prompt.Content, &prompt.IsPublic, &prompt.CreatedBy, &prompt.CreatedAt, &prompt.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.NewCBError(models.ErrCodeSessionNotFound, "system prompt not found", err)
		}
		return nil, fmt.Errorf("failed to update system prompt: %w", err)
	}

	return &prompt, nil
}

func (db *DB) DeleteSystemPrompt(ctx context.Context, id int64) error {
	query := `DELETE FROM system_prompts WHERE id = ?`

	result, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete system prompt: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "system prompt not found", nil)
	}

	return nil
}

// Session user operations

func (db *DB) AddUserToSession(ctx context.Context, sessionID int64, userID int64, role string) error {
	query := `
		INSERT INTO session_users (session_id, user_id, role)
		VALUES (?, ?, ?)
		ON CONFLICT(session_id, user_id) 
		DO UPDATE SET 
			role = excluded.role,
			joined_at = CURRENT_TIMESTAMP
	`

	_, err := db.conn.ExecContext(ctx, query, sessionID, userID, role)
	if err != nil {
		return fmt.Errorf("failed to add user to session: %w", err)
	}

	return nil
}

func (db *DB) RemoveUserFromSession(ctx context.Context, sessionID int64, userID int64) error {
	query := `DELETE FROM session_users WHERE session_id = ? AND user_id = ?`

	result, err := db.conn.ExecContext(ctx, query, sessionID, userID)
	if err != nil {
		return fmt.Errorf("failed to remove user from session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "user not found in session", nil)
	}

	return nil
}

func (db *DB) GetSessionUsers(ctx context.Context, sessionID int64) ([]*models.SessionUser, error) {
	query := `
		SELECT id, session_id, user_id, role, joined_at
		FROM session_users 
		WHERE session_id = ?
		ORDER BY joined_at ASC
	`

	rows, err := db.conn.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session users: %w", err)
	}
	defer rows.Close()

	var sessionUsers []*models.SessionUser
	for rows.Next() {
		var sessionUser models.SessionUser
		err := rows.Scan(
			&sessionUser.ID, &sessionUser.SessionID, &sessionUser.UserID, &sessionUser.Role, &sessionUser.JoinedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session user: %w", err)
		}
		sessionUsers = append(sessionUsers, &sessionUser)
	}

	return sessionUsers, nil
}

func (db *DB) GetUserRole(ctx context.Context, sessionID int64, userID int64) (string, error) {
	query := `
		SELECT role 
		FROM session_users 
		WHERE session_id = ? AND user_id = ?
	`

	var role string
	err := db.conn.QueryRowContext(ctx, query, sessionID, userID).Scan(&role)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get user role: %w", err)
	}

	return role, nil
}

func (db *DB) GetSessionOwner(ctx context.Context, sessionID int64) (int64, error) {
	query := `
		SELECT user_id 
		FROM session_users 
		WHERE session_id = ? AND role = 'owner'
	`

	var ownerID int64
	err := db.conn.QueryRowContext(ctx, query, sessionID).Scan(&ownerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, models.NewCBError(models.ErrCodeSessionNotFound, "session owner not found", err)
		}
		return 0, fmt.Errorf("failed to get session owner: %w", err)
	}

	return ownerID, nil
}

func (db *DB) CheckBranchNameExists(ctx context.Context, branchName string) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM sessions 
		WHERE branch_name = ?
	`

	var count int
	err := db.conn.QueryRowContext(ctx, query, branchName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check branch name: %w", err)
	}

	return count > 0, nil
}

func (db *DB) GetSessionByBranchName(ctx context.Context, branchName string) (*models.Session, error) {
	query := `
		SELECT id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, model_name, running_cost, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE branch_name = ?
	`

	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, branchName).Scan(
		&session.ID, &session.SessionID, &session.SlackWorkspaceID,
		&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
		&session.WorkTreePath, &session.ModelName, &session.RunningCost, &session.Status,
		&session.CreatedAt, &session.UpdatedAt, &session.EndedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.NewCBError(models.ErrCodeSessionNotFound, "session not found", err)
		}
		return nil, fmt.Errorf("failed to get session by branch name: %w", err)
	}

	return &session, nil
}

func (db *DB) IsUserAssociatedWithSession(ctx context.Context, sessionID int64, userID int64) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM session_users 
		WHERE session_id = ? AND user_id = ?
	`

	var count int
	err := db.conn.QueryRowContext(ctx, query, sessionID, userID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check user session association: %w", err)
	}

	return count > 0, nil
}

// User system prompt operations

func (db *DB) AddSystemPromptToUser(ctx context.Context, userID int64, systemPromptID int64) error {
	query := `
		INSERT INTO user_system_prompts (user_id, system_prompt_id)
		VALUES (?, ?)
		ON CONFLICT(user_id, system_prompt_id) DO NOTHING
	`

	_, err := db.conn.ExecContext(ctx, query, userID, systemPromptID)
	if err != nil {
		return fmt.Errorf("failed to add system prompt to user: %w", err)
	}

	return nil
}

func (db *DB) RemoveSystemPromptFromUser(ctx context.Context, userID int64, systemPromptID int64) error {
	query := `DELETE FROM user_system_prompts WHERE user_id = ? AND system_prompt_id = ?`

	result, err := db.conn.ExecContext(ctx, query, userID, systemPromptID)
	if err != nil {
		return fmt.Errorf("failed to remove system prompt from user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return models.NewCBError(models.ErrCodeSessionNotFound, "system prompt not found for user", nil)
	}

	return nil
}

// Transaction helper
func (db *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		} else if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	err = fn(tx)
	return err
}

