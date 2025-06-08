package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
)

// Messages streamed from Claude with the stream-json outpout format are strictly typed as follows:
// type SDKMessage =
//   // An assistant message
//   | {
//       type: "assistant";
//       message: Message; // from Anthropic SDK
//       session_id: string;
//     }
//
//   // A user message
//   | {
//       type: "user";
//       message: MessageParam; // from Anthropic SDK
//       session_id: string;
//     }
//
//   // Emitted as the last message
//   | {
//       type: "result";
//       subtype: "success";
//       cost_usd: float;
//       duration_ms: float;
//       duration_api_ms: float;
//       is_error: boolean;
//       num_turns: int;
//       result: string;
//       session_id: string;
//     }
//
//   // Emitted as the last message, when we've reached the maximum number of turns
//   | {
//       type: "result";
//       subtype: "error_max_turns";
//       cost_usd: float;
//       duration_ms: float;
//       duration_api_ms: float;
//       is_error: boolean;
//       num_turns: int;
//       session_id: string;
//     }
//
//   // Emitted as the first message at the start of a conversation
//   | {
//       type: "system";
//       subtype: "init";
//       session_id: string;
//       tools: string[];
//       mcp_servers: {
//         name: string;
//         status: string;
//       }[];
//     };

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

	// Handle stderr in goroutine
	errScanner := bufio.NewScanner(stderr)
	for errScanner.Scan() {
		line := scanner.Text()
		messageCallback(fmt.Sprintf("⚠️ %s", line))
	}

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
		inputRate = 0.003  // $3 per 1M tokens
		outputRate = 0.015 // $15 per 1M tokens
	case "opus":
		inputRate = 0.015  // $15 per 1M tokens
		outputRate = 0.075 // $75 per 1M tokens
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
	return `You are Claude Bot, a highly experienced and distinguished distributed systems engineer with proficiency in many languages, including Go, Rust, Python, JS, Java, Elixir, Haskell, Clojure, and C. You have wide and deep knowledge of distributed systems and Linux deployments of cloud services. You are an expert with AWS, often utilizing cloud native services when it is cost and time effective to do so. You are also an expert in machine learning, distributed systems, data structures, high performance programming, and low latency data processing. You have deep experience with assembly and how understand the low level computation that will result from the code you write in high level languages. You are able to analyze large datasets and extract meaningful insights. You think deeply about problems before you arrive at a solution, and consider all possible trade offs. You are able to communicate your ideas clearly and concisely to both technical and non-technical audiences. You strongly care about API design, boundaries, and how code can be simple and highly maintainable while also being elegant and generic, utilizing things like type systems and categorically removing bugs while covering edge cases by the nature of your design. You have access to this git repository and can help with coding, debugging, documentation, and other development tasks. Please be helpful, accurate, and concise in your responses.`
}

