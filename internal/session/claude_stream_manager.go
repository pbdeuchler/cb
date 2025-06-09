package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Messages streamed from Claude with the stream-json output format are strictly typed as follows:
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

// ClaudeStreamManager manages stateless Claude command execution
type ClaudeStreamManager struct{}

// ClaudeMessage represents a parsed message from Claude's stream output
type ClaudeMessage struct {
	Type      string      `json:"type"`
	Subtype   string      `json:"subtype,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Message   interface{} `json:"message,omitempty"`
	Result    string      `json:"result,omitempty"`
	CostUSD   float64     `json:"cost_usd,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
	NumTurns  int         `json:"num_turns,omitempty"`
	Tools     []string    `json:"tools,omitempty"`
}

// NewClaudeStreamManager creates a new streaming Claude manager
func NewClaudeStreamManager() *ClaudeStreamManager {
	return &ClaudeStreamManager{}
}

func buildClaudeCommand(ctx context.Context, prompt, modelName, worktreePath, apiKey, claudeSessionID string) *exec.Cmd {
	args := []string{}
	args = append(args, "-p")
	if claudeSessionID != "" {
		args = append(args, "-r", claudeSessionID)
	}
	args = append(args, "--output", "stream-json")
	args = append(args, "--model", modelName)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = worktreePath
	// Set required environment variables
	cmd.Env = append(os.Environ(),
		"DISABLE_BUG_COMMAND=1",
		"DISABLE_ERROR_REPORTING=1",
		"DISABLED_NON_ESSENTIAL_MODEL_CALLS=1",
		"DISABLE_TELEMETRY=1",
		"ANTHROPIC_API_KEY="+apiKey,
	)
	return cmd
}

// StartSession starts a new Claude session with a system prompt
func (csm *ClaudeStreamManager) StartSession(ctx context.Context, featureName, worktreePath, systemPrompt, modelName, anthropicAPIKey string, messageCallback func(string), costCallback func(float64)) (string, error) {
	cmd := buildClaudeCommand(ctx, systemPrompt, modelName, worktreePath, anthropicAPIKey, "")

	return csm.executeClaudeCommand(cmd, messageCallback, costCallback)
}

// SendMessage sends a message to an existing Claude session
func (csm *ClaudeStreamManager) SendMessage(ctx context.Context, claudeSessionID, featureName, worktreePath, message, modelName, anthropicAPIKey string, messageCallback func(string), costCallback func(float64)) error {
	cmd := buildClaudeCommand(ctx, message, modelName, worktreePath, anthropicAPIKey, claudeSessionID)

	_, err := csm.executeClaudeCommand(cmd, messageCallback, costCallback)
	return err
}

// executeClaudeCommand executes a Claude command and streams output
func (csm *ClaudeStreamManager) executeClaudeCommand(cmd *exec.Cmd, messageCallback func(string), costCallback func(float64)) (string, error) {
	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start Claude process: %w", err)
	}

	var claudeSessionID string

	// Handle stdout - parse JSON messages
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		// Try to parse as JSON first
		var msg ClaudeMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// If not JSON, send as regular message
			messageCallback(line)
			continue
		}

		// Handle different message types based on the schema
		switch msg.Type {
		case "system":
			if msg.Subtype == "init" {
				claudeSessionID = msg.SessionID
				messageCallback(fmt.Sprintf("🔧 Claude session initialized: %s", msg.SessionID))
			}
		case "assistant":
			// Forward assistant messages
			if msg.Message != nil {
				messageCallback(fmt.Sprintf("🤖 %v", msg.Message))
			}
		case "user":
			// Forward user messages (for debugging)
			if msg.Message != nil {
				messageCallback(fmt.Sprintf("👤 %v", msg.Message))
			}
		case "result":
			if msg.Subtype == "success" {
				messageCallback(fmt.Sprintf("✅ %s", msg.Result))
				// Update cost when available from Claude
				if msg.CostUSD > 0 {
					costCallback(msg.CostUSD)
				}
			} else if msg.Subtype == "error_max_turns" {
				messageCallback("❌ Maximum turns reached")
				// Update cost when available from Claude
				if msg.CostUSD > 0 {
					costCallback(msg.CostUSD)
				}
			}
		default:
			// Forward any other messages
			messageCallback(line)
		}
	}

	if err := scanner.Err(); err != nil {
		messageCallback(fmt.Sprintf("❌ Stream error: %v", err))
	}

	// Handle stderr - forward all stderr output
	errScanner := bufio.NewScanner(stderr)
	for errScanner.Scan() {
		line := errScanner.Text()
		messageCallback(fmt.Sprintf("⚠️ %s", line))
	}

	// Wait for command to complete
	if err := cmd.Wait(); err != nil {
		return claudeSessionID, fmt.Errorf("Claude command failed: %w", err)
	}

	return claudeSessionID, nil
}

// GetDefaultSystemPrompt returns a default system prompt
func (csm *ClaudeStreamManager) GetDefaultSystemPrompt() string {
	return `You are Claude Bot, a highly experienced and distinguished distributed systems engineer with proficiency in many languages, including Go, Rust, Python, JS, Java, Elixir, Haskell, Clojure, and C. You have wide and deep knowledge of distributed systems and Linux deployments of cloud services. You are an expert with AWS, often utilizing cloud native services when it is cost and time effective to do so. You are also an expert in machine learning, distributed systems, data structures, high performance programming, and low latency data processing. You have deep experience with assembly and how understand the low level computation that will result from the code you write in high level languages. You are able to analyze large datasets and extract meaningful insights. You think deeply about problems before you arrive at a solution, and consider all possible trade offs. You are able to communicate your ideas clearly and concisely to both technical and non-technical audiences. You strongly care about API design, boundaries, and how code can be simple and highly maintainable while also being elegant and generic, utilizing things like type systems and categorically removing bugs while covering edge cases by the nature of your design. You have access to this git repository and can help with coding, debugging, documentation, and other development tasks. Please be helpful, accurate, and concise in your responses.`
}
