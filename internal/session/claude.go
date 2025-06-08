package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// ClaudeManager manages Claude Code processes
type ClaudeManager struct {
	claudeCodePath string
	processes      map[string]*ClaudeProcess
	mu             sync.RWMutex
}

// ClaudeProcess represents a running Claude Code process
type ClaudeProcess struct {
	PID       int
	SessionID string
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	Stdout    io.ReadCloser
	Stderr    io.ReadCloser
	StartedAt time.Time
	Status    string
	mu        sync.RWMutex
	
	// Channel for receiving output
	OutputChan chan string
	ErrorChan  chan error
	
	// Shutdown handling
	done       chan struct{}
	cancelFunc context.CancelFunc
}

// NewClaudeManager creates a new Claude manager
func NewClaudeManager(claudeCodePath string) *ClaudeManager {
	return &ClaudeManager{
		claudeCodePath: claudeCodePath,
		processes:      make(map[string]*ClaudeProcess),
	}
}

// StartSession starts a new Claude Code session
func (cm *ClaudeManager) StartSession(ctx context.Context, sessionID, workDir, apiKey string) (*ClaudeProcess, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// Check if session already exists
	if _, exists := cm.processes[sessionID]; exists {
		return nil, models.NewCBError(models.ErrCodeSessionExists, "Claude session already exists", nil)
	}
	
	// Create context with cancellation for this process
	processCtx, cancel := context.WithCancel(ctx)
	
	// Prepare command
	cmd := exec.CommandContext(processCtx, cm.claudeCodePath,
		"--headless",
		"--api-key", apiKey,
		"--work-dir", workDir,
		"--enable-mcp-servers",
	)
	
	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		cancel()
		return nil, fmt.Errorf("failed to start Claude process: %w", err)
	}
	
	// Create process wrapper
	process := &ClaudeProcess{
		PID:        cmd.Process.Pid,
		SessionID:  sessionID,
		Cmd:        cmd,
		Stdin:      stdin,
		Stdout:     stdout,
		Stderr:     stderr,
		StartedAt:  time.Now(),
		Status:     "running",
		OutputChan: make(chan string, 100),
		ErrorChan:  make(chan error, 10),
		done:       make(chan struct{}),
		cancelFunc: cancel,
	}
	
	// Store process
	cm.processes[sessionID] = process
	
	// Start output readers
	go process.readOutput()
	go process.readErrors()
	go process.waitForExit()
	
	log.Printf("Started Claude session %s with PID %d", sessionID, process.PID)
	
	return process, nil
}

// GetSession returns a Claude process by session ID
func (cm *ClaudeManager) GetSession(sessionID string) (*ClaudeProcess, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	process, exists := cm.processes[sessionID]
	if !exists {
		return nil, models.NewCBError(models.ErrCodeSessionNotFound, "Claude session not found", nil)
	}
	
	return process, nil
}

// SendCommand sends a command to a Claude session
func (cm *ClaudeManager) SendCommand(ctx context.Context, sessionID, command string) (string, error) {
	process, err := cm.GetSession(sessionID)
	if err != nil {
		return "", err
	}
	
	return process.SendCommand(ctx, command)
}

// StopSession stops a Claude session
func (cm *ClaudeManager) StopSession(ctx context.Context, sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	process, exists := cm.processes[sessionID]
	if !exists {
		return models.NewCBError(models.ErrCodeSessionNotFound, "Claude session not found", nil)
	}
	
	// Remove from active processes immediately
	delete(cm.processes, sessionID)
	
	return process.Stop(ctx)
}

// StopAllSessions stops all active Claude sessions
func (cm *ClaudeManager) StopAllSessions(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	var errors []error
	
	for sessionID, process := range cm.processes {
		if err := process.Stop(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop session %s: %w", sessionID, err))
		}
	}
	
	// Clear all processes
	cm.processes = make(map[string]*ClaudeProcess)
	
	if len(errors) > 0 {
		return fmt.Errorf("errors stopping sessions: %v", errors)
	}
	
	return nil
}

// GetActiveSessionCount returns the number of active sessions
func (cm *ClaudeManager) GetActiveSessionCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.processes)
}

// ClaudeProcess methods

// SendCommand sends a command to the Claude process
func (cp *ClaudeProcess) SendCommand(ctx context.Context, command string) (string, error) {
	cp.mu.RLock()
	status := cp.Status
	cp.mu.RUnlock()
	
	if status != "running" {
		return "", models.NewCBError(models.ErrCodeClaudeUnavailable, "Claude process not running", nil)
	}
	
	// Send command
	_, err := cp.Stdin.Write([]byte(command + "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}
	
	// Wait for response with timeout
	select {
	case output := <-cp.OutputChan:
		return output, nil
	case err := <-cp.ErrorChan:
		return "", fmt.Errorf("Claude process error: %w", err)
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(30 * time.Second):
		return "", fmt.Errorf("command timeout")
	}
}

// Stop gracefully stops the Claude process
func (cp *ClaudeProcess) Stop(ctx context.Context) error {
	cp.mu.Lock()
	if cp.Status == "stopped" {
		cp.mu.Unlock()
		return nil
	}
	cp.Status = "stopping"
	cp.mu.Unlock()
	
	log.Printf("Stopping Claude session %s (PID %d)", cp.SessionID, cp.PID)
	
	// Try graceful shutdown first
	if cp.Stdin != nil {
		cp.Stdin.Write([]byte("exit\n"))
		cp.Stdin.Close()
	}
	
	// Wait for graceful exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- cp.Cmd.Wait()
	}()
	
	select {
	case <-done:
		// Process exited gracefully
	case <-time.After(5 * time.Second):
		// Force kill
		log.Printf("Force killing Claude process %d", cp.PID)
		if err := cp.Cmd.Process.Signal(syscall.SIGKILL); err != nil {
			log.Printf("Failed to kill process: %v", err)
		}
		<-done // Wait for process to be reaped
	}
	
	// Cancel context and close channels
	cp.cancelFunc()
	close(cp.done)
	
	cp.mu.Lock()
	cp.Status = "stopped"
	cp.mu.Unlock()
	
	log.Printf("Claude session %s stopped", cp.SessionID)
	return nil
}

// IsRunning checks if the Claude process is still running
func (cp *ClaudeProcess) IsRunning() bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.Status == "running"
}

// GetStatus returns the current status of the process
func (cp *ClaudeProcess) GetStatus() string {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.Status
}

// readOutput reads stdout from the Claude process
func (cp *ClaudeProcess) readOutput() {
	defer close(cp.OutputChan)
	
	scanner := bufio.NewScanner(cp.Stdout)
	for scanner.Scan() {
		select {
		case cp.OutputChan <- scanner.Text():
		case <-cp.done:
			return
		}
	}
	
	if err := scanner.Err(); err != nil {
		select {
		case cp.ErrorChan <- fmt.Errorf("stdout read error: %w", err):
		case <-cp.done:
		}
	}
}

// readErrors reads stderr from the Claude process
func (cp *ClaudeProcess) readErrors() {
	scanner := bufio.NewScanner(cp.Stderr)
	for scanner.Scan() {
		select {
		case cp.ErrorChan <- fmt.Errorf("Claude stderr: %s", scanner.Text()):
		case <-cp.done:
			return
		}
	}
	
	if err := scanner.Err(); err != nil {
		select {
		case cp.ErrorChan <- fmt.Errorf("stderr read error: %w", err):
		case <-cp.done:
		}
	}
}

// waitForExit waits for the process to exit and updates status
func (cp *ClaudeProcess) waitForExit() {
	err := cp.Cmd.Wait()
	
	cp.mu.Lock()
	if cp.Status == "running" {
		if err != nil {
			cp.Status = "error"
			log.Printf("Claude process %d exited with error: %v", cp.PID, err)
		} else {
			cp.Status = "stopped"
			log.Printf("Claude process %d exited normally", cp.PID)
		}
	}
	cp.mu.Unlock()
	
	// Close pipes
	if cp.Stdout != nil {
		cp.Stdout.Close()
	}
	if cp.Stderr != nil {
		cp.Stderr.Close()
	}
}