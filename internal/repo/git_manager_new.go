package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// GoGitManager creates a new Git manager using go-git
type GoGitManager struct {
	reposDir     string
	worktreesDir string
}

// NewGoGitManager creates a new Git manager using go-git
func NewGoGitManager() *GoGitManager {
	homeDir, _ := os.UserHomeDir()
	return &GoGitManager{
		reposDir:     filepath.Join(homeDir, ".claude-bot", "repos"),
		worktreesDir: filepath.Join(homeDir, ".claude-bot", "worktrees"),
	}
}

// SessionSetupResult contains the result of setting up a session
type SessionSetupResult struct {
	WorktreePath string
	Messages     []string
}

// SetupSessionRepo sets up a repository and worktree for a session
func (gm *GoGitManager) SetupSessionRepo(ctx context.Context, repoURL, fromCommitish, featureName string, progressCallback func(string)) (*SessionSetupResult, error) {
	var messages []string
	
	// Ensure directories exist
	if err := os.MkdirAll(gm.reposDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repos directory: %w", err)
	}
	if err := os.MkdirAll(gm.worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	// Extract repo name from URL
	repoName := extractRepoName(repoURL)
	repoPath := filepath.Join(gm.reposDir, repoName)
	worktreePath := filepath.Join(gm.worktreesDir, featureName)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, fmt.Errorf("worktree already exists for feature '%s'", featureName)
	}

	var repo *git.Repository
	var err error

	// Check if repo exists locally
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		// Clone the repository
		msg := fmt.Sprintf("🔄 Cloning repository %s...", repoURL)
		messages = append(messages, msg)
		progressCallback(msg)

		repo, err = git.PlainClone(repoPath, false, &git.CloneOptions{
			URL:      repoURL,
			Progress: os.Stdout,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to clone repository: %w", err)
		}

		msg = "✅ Repository cloned successfully"
		messages = append(messages, msg)
		progressCallback(msg)
	} else {
		// Open existing repository
		msg := "📂 Opening existing repository..."
		messages = append(messages, msg)
		progressCallback(msg)

		repo, err = git.PlainOpen(repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open repository: %w", err)
		}

		// Fetch latest changes
		msg = "🔄 Fetching latest changes from origin..."
		messages = append(messages, msg)
		progressCallback(msg)

		err = repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return nil, fmt.Errorf("failed to fetch from origin: %w", err)
		}

		msg = "✅ Repository updated"
		messages = append(messages, msg)
		progressCallback(msg)
	}

	// Check if feature branch already exists
	branches, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	err = branches.ForEach(func(ref *plumbing.Reference) error {
		branchName := ref.Name().Short()
		if branchName == featureName {
			return fmt.Errorf("branch '%s' already exists", featureName)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Resolve the commitish
	msg := fmt.Sprintf("🔍 Resolving commitish '%s'...", fromCommitish)
	messages = append(messages, msg)
	progressCallback(msg)

	hash, err := repo.ResolveRevision(plumbing.Revision(fromCommitish))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve commitish '%s': %w", fromCommitish, err)
	}

	// Create worktree from the commitish
	msg = fmt.Sprintf("🌿 Creating worktree for feature '%s'...", featureName)
	messages = append(messages, msg)
	progressCallback(msg)

	mainWorktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get main worktree: %w", err)
	}

	// Checkout the specific commit
	err = mainWorktree.Checkout(&git.CheckoutOptions{
		Hash: *hash,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to checkout commitish: %w", err)
	}

	// Create new branch from current state
	newBranchRef := plumbing.NewBranchReferenceName(featureName)
	newRef := plumbing.NewHashReference(newBranchRef, *hash)
	err = repo.Storer.SetReference(newRef)
	if err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	// Checkout the new branch
	err = mainWorktree.Checkout(&git.CheckoutOptions{
		Branch: newBranchRef,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to checkout new branch: %w", err)
	}

	// Create the actual worktree directory by copying
	err = copyDir(repoPath, worktreePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	msg = "✅ Worktree created successfully"
	messages = append(messages, msg)
	progressCallback(msg)

	return &SessionSetupResult{
		WorktreePath: worktreePath,
		Messages:     messages,
	}, nil
}

// extractRepoName extracts repository name from URL
func extractRepoName(repoURL string) string {
	// Remove .git suffix if present
	name := strings.TrimSuffix(repoURL, ".git")
	
	// Extract the last part of the path
	parts := strings.Split(name, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	
	return "unknown-repo"
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory to avoid conflicts
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Calculate destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = dstFile.ReadFrom(srcFile)
		return err
	})
}

// Cleanup removes the worktree directory
func (gm *GoGitManager) Cleanup(ctx context.Context, worktreePath string) error {
	return os.RemoveAll(worktreePath)
}

// ValidateRepoURL validates if the repository URL is accessible
func (gm *GoGitManager) ValidateRepoURL(ctx context.Context, repoURL string) error {
	// For now, just check if it's a valid git URL format
	if !strings.Contains(repoURL, "github.com") && !strings.Contains(repoURL, ".git") {
		return fmt.Errorf("invalid repository URL format")
	}
	return nil
}