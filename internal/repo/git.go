package repo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// GitManager handles Git repository operations
type GitManager struct {
	gitPath string
}

// NewGitManager creates a new Git manager
func NewGitManager() *GitManager {
	return &GitManager{
		gitPath: "git", // Assume git is in PATH
	}
}

// CloneOrCreateWorkTree clones a repository or creates a work tree
func (gm *GitManager) CloneOrCreateWorkTree(ctx context.Context, repoURL, branch, workDir string) error {
	// Check if directory already exists
	if _, err := os.Stat(workDir); err == nil {
		// Directory exists, check if it's a valid git repo
		if gm.isGitRepo(workDir) {
			// Update existing repo
			return gm.updateRepo(ctx, workDir, branch)
		}
		// Remove existing directory if it's not a git repo
		if err := os.RemoveAll(workDir); err != nil {
			return fmt.Errorf("failed to remove existing directory: %w", err)
		}
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(workDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Clone the repository
	cmd := exec.CommandContext(ctx, gm.gitPath, "clone", "--depth", "1", "--branch", branch, repoURL, workDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		// If branch doesn't exist, try cloning default branch and then checkout
		if strings.Contains(string(output), "not found") {
			if err := gm.cloneAndCheckout(ctx, repoURL, branch, workDir); err != nil {
				return fmt.Errorf("failed to clone repository: %w", err)
			}
		} else {
			return fmt.Errorf("failed to clone repository: %w, output: %s", err, output)
		}
	}

	return nil
}

// cloneAndCheckout clones the repo and then checks out the specified branch
func (gm *GitManager) cloneAndCheckout(ctx context.Context, repoURL, branch, workDir string) error {
	// Clone without specifying branch
	cmd := exec.CommandContext(ctx, gm.gitPath, "clone", repoURL, workDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %w, output: %s", err, output)
	}

	// Change to the work directory
	oldDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("failed to change to work directory: %w", err)
	}

	// Check if branch exists
	cmd = exec.CommandContext(ctx, gm.gitPath, "rev-parse", "--verify", "origin/"+branch)
	if err := cmd.Run(); err != nil {
		// Branch doesn't exist, create it
		cmd = exec.CommandContext(ctx, gm.gitPath, "checkout", "-b", branch)
	} else {
		// Branch exists, check it out
		cmd = exec.CommandContext(ctx, gm.gitPath, "checkout", "-b", branch, "origin/"+branch)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w, output: %s", branch, err, output)
	}

	return nil
}

// updateRepo updates an existing repository
func (gm *GitManager) updateRepo(ctx context.Context, workDir, branch string) error {
	oldDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("failed to change to work directory: %w", err)
	}

	// Fetch latest changes
	cmd := exec.CommandContext(ctx, gm.gitPath, "fetch", "origin")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch from origin: %w, output: %s", err, output)
	}

	// Checkout the desired branch
	cmd = exec.CommandContext(ctx, gm.gitPath, "checkout", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		// If branch doesn't exist locally, create it from origin
		cmd = exec.CommandContext(ctx, gm.gitPath, "checkout", "-b", branch, "origin/"+branch)
		if output2, err2 := cmd.CombinedOutput(); err2 != nil {
			return fmt.Errorf("failed to checkout branch %s: %w, output: %s, %s", branch, err2, output, output2)
		}
	}

	// Pull latest changes
	cmd = exec.CommandContext(ctx, gm.gitPath, "pull", "origin", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull latest changes: %w, output: %s", err, output)
	}

	return nil
}

// CommitAndPush commits all changes and pushes to the remote repository
func (gm *GitManager) CommitAndPush(ctx context.Context, workDir, branch, message string) error {
	oldDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("failed to change to work directory: %w", err)
	}

	// Check if there are any changes to commit
	cmd := exec.CommandContext(ctx, gm.gitPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if len(strings.TrimSpace(string(output))) == 0 {
		// No changes to commit
		return nil
	}

	// Add all changes
	cmd = exec.CommandContext(ctx, gm.gitPath, "add", ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add changes: %w, output: %s", err, output)
	}

	// Configure git user if not set
	if err := gm.configureGitUser(ctx); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to configure git user: %v\n", err)
	}

	// Commit changes
	cmd = exec.CommandContext(ctx, gm.gitPath, "commit", "-m", message)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to commit changes: %w, output: %s", err, output)
	}

	// Push changes
	cmd = exec.CommandContext(ctx, gm.gitPath, "push", "origin", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push changes: %w, output: %s", err, output)
	}

	return nil
}

// Cleanup removes the work directory
func (gm *GitManager) Cleanup(ctx context.Context, workDir string) error {
	if err := os.RemoveAll(workDir); err != nil {
		return fmt.Errorf("failed to cleanup work directory: %w", err)
	}
	return nil
}

// GetRepoInfo returns information about the repository
func (gm *GitManager) GetRepoInfo(ctx context.Context, workDir string) (map[string]string, error) {
	oldDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(workDir); err != nil {
		return nil, fmt.Errorf("failed to change to work directory: %w", err)
	}

	info := make(map[string]string)

	// Get current branch
	cmd := exec.CommandContext(ctx, gm.gitPath, "rev-parse", "--abbrev-ref", "HEAD")
	if output, err := cmd.Output(); err == nil {
		info["branch"] = strings.TrimSpace(string(output))
	}

	// Get current commit hash
	cmd = exec.CommandContext(ctx, gm.gitPath, "rev-parse", "HEAD")
	if output, err := cmd.Output(); err == nil {
		info["commit"] = strings.TrimSpace(string(output))
	}

	// Get remote URL
	cmd = exec.CommandContext(ctx, gm.gitPath, "remote", "get-url", "origin")
	if output, err := cmd.Output(); err == nil {
		info["remote"] = strings.TrimSpace(string(output))
	}

	// Get repository status
	cmd = exec.CommandContext(ctx, gm.gitPath, "status", "--porcelain")
	if output, err := cmd.Output(); err == nil {
		if len(strings.TrimSpace(string(output))) == 0 {
			info["status"] = "clean"
		} else {
			info["status"] = "dirty"
		}
	}

	return info, nil
}

// ValidateRepoURL validates that a repository URL is accessible
func (gm *GitManager) ValidateRepoURL(ctx context.Context, repoURL string) error {
	cmd := exec.CommandContext(ctx, gm.gitPath, "ls-remote", "--heads", repoURL)
	if output, err := cmd.CombinedOutput(); err != nil {
		return models.NewCBError(models.ErrCodeRepoAccess, 
			fmt.Sprintf("repository not accessible: %s", repoURL), 
			fmt.Errorf("git ls-remote failed: %w, output: %s", err, output))
	}
	return nil
}

// isGitRepo checks if a directory is a git repository
func (gm *GitManager) isGitRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	if stat, err := os.Stat(gitDir); err == nil {
		return stat.IsDir()
	}
	return false
}

// configureGitUser configures git user if not already set
func (gm *GitManager) configureGitUser(ctx context.Context) error {
	// Check if user.name is set
	cmd := exec.CommandContext(ctx, gm.gitPath, "config", "user.name")
	if err := cmd.Run(); err != nil {
		// Set default user name
		cmd = exec.CommandContext(ctx, gm.gitPath, "config", "user.name", "Claude Bot")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set git user.name: %w", err)
		}
	}

	// Check if user.email is set
	cmd = exec.CommandContext(ctx, gm.gitPath, "config", "user.email")
	if err := cmd.Run(); err != nil {
		// Set default user email
		cmd = exec.CommandContext(ctx, gm.gitPath, "config", "user.email", "claude-bot@example.com")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set git user.email: %w", err)
		}
	}

	return nil
}

// CreateBranch creates a new branch from the current branch
func (gm *GitManager) CreateBranch(ctx context.Context, workDir, branchName string) error {
	oldDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("failed to change to work directory: %w", err)
	}

	// Create and checkout new branch
	cmd := exec.CommandContext(ctx, gm.gitPath, "checkout", "-b", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create branch %s: %w, output: %s", branchName, err, output)
	}

	return nil
}

// ListBranches lists all branches in the repository
func (gm *GitManager) ListBranches(ctx context.Context, workDir string) ([]string, error) {
	oldDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(workDir); err != nil {
		return nil, fmt.Errorf("failed to change to work directory: %w", err)
	}

	// List all branches
	cmd := exec.CommandContext(ctx, gm.gitPath, "branch", "-a")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var branches []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Remove current branch indicator and remote prefixes
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "remotes/origin/")
		if !strings.HasPrefix(line, "HEAD") {
			branches = append(branches, line)
		}
	}

	return branches, nil
}