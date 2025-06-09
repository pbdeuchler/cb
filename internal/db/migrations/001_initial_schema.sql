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
    UNIQUE(user_id, credential_type),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Sessions table (no direct user relationship - uses session_users junction table)
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT DEFAULT '',
    slack_workspace_id TEXT NOT NULL,
    slack_channel_id TEXT NOT NULL,
    slack_thread_ts TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    branch_name TEXT NOT NULL,
    work_tree_path TEXT NOT NULL,
    model_name TEXT NOT NULL DEFAULT 'sonnet',
    running_cost REAL NOT NULL DEFAULT 0.0,
    status TEXT NOT NULL CHECK(status IN ('starting', 'active', 'ending', 'ended', 'error')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMP,
    UNIQUE(branch_name),
    UNIQUE(work_tree_path),
    UNIQUE(slack_channel_id, slack_thread_ts)
);

-- System prompts table
CREATE TABLE IF NOT EXISTS system_prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);

-- User system prompts junction table (many-to-many)
CREATE TABLE IF NOT EXISTS user_system_prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    system_prompt_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, system_prompt_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (system_prompt_id) REFERENCES system_prompts(id) ON DELETE CASCADE
);

-- Session users junction table (many-to-many)
CREATE TABLE IF NOT EXISTS session_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('owner', 'collaborator')),
    joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(session_id, user_id),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
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
CREATE INDEX IF NOT EXISTS idx_system_prompts_created_by ON system_prompts(created_by);
CREATE INDEX IF NOT EXISTS idx_system_prompts_public ON system_prompts(is_public) WHERE is_public = TRUE;
CREATE INDEX IF NOT EXISTS idx_system_prompts_name ON system_prompts(name);
CREATE INDEX IF NOT EXISTS idx_user_system_prompts_user ON user_system_prompts(user_id);
CREATE INDEX IF NOT EXISTS idx_user_system_prompts_prompt ON user_system_prompts(system_prompt_id);
CREATE INDEX IF NOT EXISTS idx_session_users_session ON session_users(session_id);
CREATE INDEX IF NOT EXISTS idx_session_users_user ON session_users(user_id);
CREATE INDEX IF NOT EXISTS idx_session_users_role ON session_users(role);
