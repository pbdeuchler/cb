-- Users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slack_workspace_id TEXT NOT NULL,
    slack_user_id TEXT NOT NULL,
    slack_user_name TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(slack_workspace_id, slack_user_id)
);

-- Credentials table
CREATE TABLE IF NOT EXISTS credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    credential_type TEXT NOT NULL CHECK(credential_type IN ('anthropic', 'github')),
    credential_value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
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
CREATE TABLE IF NOT EXISTS session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    slack_message_ts TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('user_to_claude', 'claude_to_user')),
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_users_slack ON users(slack_workspace_id, slack_user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(status) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_sessions_channel ON sessions(slack_workspace_id, slack_channel_id, slack_thread_ts);
CREATE INDEX IF NOT EXISTS idx_session_messages_session ON session_messages(session_id);
CREATE INDEX IF NOT EXISTS idx_session_messages_ts ON session_messages(slack_message_ts);