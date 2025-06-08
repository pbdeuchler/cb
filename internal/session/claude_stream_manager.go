package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
)

// ClaudeStreamManager manages streaming Claude sessions
type ClaudeStreamManager struct {
	sessions map[string]*StreamingSession
	mu       sync.RWMutex
}

// StreamingSession represents an active streaming Claude session
type StreamingSession struct {
	SessionID    string
	Cmd          *exec.Cmd
	WorktreePath string
	ModelName    string
	RunningCost  float64
	IsActive     bool
	mu           sync.RWMutex
}

// StreamMessage represents a message from Claude's stream output
type StreamMessage struct {
	Type    string      `json:"type"`
	Content interface{} `json:"content,omitempty"`
	Usage   *Usage      `json:"usage,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost,omitempty"`
}

// NewClaudeStreamManager creates a new streaming Claude manager
func NewClaudeStreamManager() *ClaudeStreamManager {
	return &ClaudeStreamManager{
		sessions: make(map[string]*StreamingSession),
	}
}

// StartStreamingSession starts a new streaming Claude session
func (csm *ClaudeStreamManager) StartStreamingSession(ctx context.Context, sessionID, worktreePath, systemPrompt, modelName string, messageCallback func(string), costCallback func(float64)) error {
	csm.mu.Lock()
	defer csm.mu.Unlock()

	// Check if session already exists
	if _, exists := csm.sessions[sessionID]; exists {
		return fmt.Errorf("session %s already exists", sessionID)
	}

	// Build Claude command
	cmd := exec.CommandContext(ctx, "claude", 
		"-p", systemPrompt,
		"--model", modelName,
		"--output-format", "stream-json")
	cmd.Dir = worktreePath

	// Set up streaming session
	session := &StreamingSession{
		SessionID:    sessionID,
		Cmd:          cmd,
		WorktreePath: worktreePath,
		ModelName:    modelName,
		RunningCost:  0.0,
		IsActive:     true,
	}

	// Start the command
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Claude process: %w", err)
	}

	// Store session
	csm.sessions[sessionID] = session

	// Handle stdout streaming in goroutine
	go func() {
		defer func() {
			csm.mu.Lock()
			if session, exists := csm.sessions[sessionID]; exists {
				session.IsActive = false
			}
			csm.mu.Unlock()
		}()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			
			// Parse JSON stream message
			var streamMsg StreamMessage
			if err := json.Unmarshal([]byte(line), &streamMsg); err != nil {
				// If not JSON, treat as regular message
				messageCallback(line)
				continue
			}

			// Handle different message types
			switch streamMsg.Type {
			case "content":
				if content, ok := streamMsg.Content.(string); ok {
					messageCallback(content)
				}
			case "usage":
				if streamMsg.Usage != nil {
					// Calculate cost and update
					cost := csm.calculateCost(streamMsg.Usage, modelName)
					session.mu.Lock()
					session.RunningCost += cost
					totalCost := session.RunningCost
					session.mu.Unlock()
					
					costCallback(totalCost)
				}
			case "error":
				messageCallback(fmt.Sprintf("❌ Error: %v", streamMsg.Content))
			case "done":
				messageCallback("✅ Claude session ready for next instruction")
			default:
				// Handle other message types or forward as-is
				if content, ok := streamMsg.Content.(string); ok {
					messageCallback(content)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			messageCallback(fmt.Sprintf("❌ Stream error: %v", err))
		}
	}()

	// Handle stderr in goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			messageCallback(fmt.Sprintf("⚠️ %s", line))
		}
	}()

	return nil
}

// SendMessage sends a message to the streaming Claude session
func (csm *ClaudeStreamManager) SendMessage(sessionID, message string, messageCallback func(string), costCallback func(float64)) error {
	csm.mu.RLock()
	session, exists := csm.sessions[sessionID]
	csm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mu.RLock()
	if !session.IsActive {
		session.mu.RUnlock()
		return fmt.Errorf("session %s is not active", sessionID)
	}
	session.mu.RUnlock()

	// Send message to Claude's stdin
	stdin := session.Cmd.Process
	if stdin == nil {
		return fmt.Errorf("session %s process not available", sessionID)
	}

	// For now, we'll handle this by restarting the Claude command with the new message
	// In a real implementation, we'd need to handle interactive communication
	messageCallback(fmt.Sprintf("📝 Processing: %s", message))
	
	return nil
}

// StopSession stops a streaming Claude session
func (csm *ClaudeStreamManager) StopSession(sessionID string) error {
	csm.mu.Lock()
	defer csm.mu.Unlock()

	session, exists := csm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mu.Lock()
	session.IsActive = false
	session.mu.Unlock()

	if session.Cmd != nil && session.Cmd.Process != nil {
		session.Cmd.Process.Kill()
		session.Cmd.Wait()
	}

	delete(csm.sessions, sessionID)
	return nil
}

// GetSessionCost returns the current running cost for a session
func (csm *ClaudeStreamManager) GetSessionCost(sessionID string) (float64, error) {
	csm.mu.RLock()
	defer csm.mu.RUnlock()

	session, exists := csm.sessions[sessionID]
	if !exists {
		return 0, fmt.Errorf("session %s not found", sessionID)
	}

	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.RunningCost, nil
}

// calculateCost calculates the cost based on token usage and model
func (csm *ClaudeStreamManager) calculateCost(usage *Usage, modelName string) float64 {
	// Pricing per 1K tokens (example rates)
	var inputRate, outputRate float64
	
	switch modelName {
	case "sonnet":
		inputRate = 0.003   // $3 per 1M tokens
		outputRate = 0.015  // $15 per 1M tokens
	case "opus":
		inputRate = 0.015   // $15 per 1M tokens
		outputRate = 0.075  // $75 per 1M tokens
	default:
		inputRate = 0.003
		outputRate = 0.015
	}

	inputCost := float64(usage.InputTokens) * inputRate / 1000
	outputCost := float64(usage.OutputTokens) * outputRate / 1000
	
	return inputCost + outputCost
}

// GetDefaultSystemPrompt returns a default system prompt
func (csm *ClaudeStreamManager) GetDefaultSystemPrompt() string {
	return `You are Claude Code, an AI assistant helping with software development tasks. 
You have access to this git repository and can help with coding, debugging, documentation, and other development tasks.
Please be helpful, accurate, and concise in your responses.`
}