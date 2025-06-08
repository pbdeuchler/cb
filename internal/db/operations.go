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
			return nil, models.NewCBError(models.ErrCodeSessionNotFound, "user not found", err)
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
			user_id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			repo_url, branch_name, work_tree_path, claude_process_pid, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	
	_, err := db.conn.ExecContext(ctx, query,
		session.UserID, session.SessionID, session.SlackWorkspaceID, session.SlackChannelID,
		session.SlackThreadTS, session.RepoURL, session.BranchName, session.WorkTreePath,
		session.ClaudeProcessPID, session.Status,
	)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	
	return nil
}

func (db *DB) GetSession(ctx context.Context, sessionID string) (*models.Session, error) {
	query := `
		SELECT id, user_id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, claude_process_pid, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE session_id = ?
	`
	
	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID, &session.UserID, &session.SessionID, &session.SlackWorkspaceID,
		&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
		&session.WorkTreePath, &session.ClaudeProcessPID, &session.Status,
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
		SELECT id, user_id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, claude_process_pid, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE slack_workspace_id = ? AND slack_channel_id = ? AND slack_thread_ts = ? AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1
	`
	
	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, workspaceID, channelID, threadTS).Scan(
		&session.ID, &session.UserID, &session.SessionID, &session.SlackWorkspaceID,
		&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
		&session.WorkTreePath, &session.ClaudeProcessPID, &session.Status,
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
		SELECT id, user_id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, claude_process_pid, status,
			   created_at, updated_at, ended_at
		FROM sessions 
		WHERE user_id = ? AND status = 'active'
		ORDER BY created_at DESC
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
			&session.ID, &session.UserID, &session.SessionID, &session.SlackWorkspaceID,
			&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
			&session.WorkTreePath, &session.ClaudeProcessPID, &session.Status,
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

func (db *DB) UpdateSessionPID(ctx context.Context, sessionID string, pid int) error {
	query := `
		UPDATE sessions 
		SET claude_process_pid = ?, updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ?
	`
	
	_, err := db.conn.ExecContext(ctx, query, pid, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session PID: %w", err)
	}
	
	return nil
}

func (db *DB) GetAllActiveSessions(ctx context.Context) ([]*models.Session, error) {
	query := `
		SELECT id, user_id, session_id, slack_workspace_id, slack_channel_id, slack_thread_ts,
			   repo_url, branch_name, work_tree_path, claude_process_pid, status,
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
			&session.ID, &session.UserID, &session.SessionID, &session.SlackWorkspaceID,
			&session.SlackChannelID, &session.SlackThreadTS, &session.RepoURL, &session.BranchName,
			&session.WorkTreePath, &session.ClaudeProcessPID, &session.Status,
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