package slack

import (
	"flag"
	"fmt"
	"strings"

	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// StartCommandArgs represents parsed start command arguments
type StartCommandArgs struct {
	RepoURL   string
	From      string
	Feature   string
	Model     string
	Prompt    string
	PName     string
}

// ParseStartCommandNew parses the new start command syntax using the flag package
func ParseStartCommandNew(text string) (*StartCommandArgs, error) {
	// Remove the bot mention and "start" command from the text
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, "start command requires arguments", nil)
	}

	// Find the start of the command arguments (after "@bot start")
	var argStart int
	for i, part := range parts {
		if part == "start" {
			argStart = i + 1
			break
		}
	}

	if argStart >= len(parts) {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, "start command requires arguments", nil)
	}

	// Get the arguments after "start"
	args := parts[argStart:]

	// Create a new flag set for parsing
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{}) // Suppress default error output

	// Define flags
	repo := fs.String("repo", "", "Git repository URL")
	from := fs.String("from", "", "Git commitish to checkout from")
	feat := fs.String("feat", "", "Feature name (becomes session identifier)")
	model := fs.String("model", "", "Model name (sonnet or opus)")
	prompt := fs.String("prompt", "", "System prompt text")
	pname := fs.String("pname", "", "System prompt name")

	// Parse the arguments
	err := fs.Parse(args)
	if err != nil {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, fmt.Sprintf("failed to parse start command: %v", err), err)
	}

	// Validate required arguments
	if *repo == "" {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, "--repo is required", nil)
	}
	if *from == "" {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, "--from is required", nil)
	}
	if *feat == "" {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, "--feat is required", nil)
	}
	if *model == "" {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, "--model is required", nil)
	}

	// Validate model name
	if *model != models.ModelSonnet && *model != models.ModelOpus {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			fmt.Sprintf("invalid model '%s', must be 'sonnet' or 'opus'", *model), nil)
	}

	// Validate that either prompt or pname is provided (but not both)
	if *prompt != "" && *pname != "" {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			"cannot specify both --prompt and --pname", nil)
	}

	return &StartCommandArgs{
		RepoURL: *repo,
		From:    *from,
		Feature: *feat,
		Model:   *model,
		Prompt:  *prompt,
		PName:   *pname,
	}, nil
}

// ValidateFeatureName ensures the feature name is valid for use as a git branch name
func ValidateFeatureName(name string) error {
	if name == "" {
		return fmt.Errorf("feature name cannot be empty")
	}

	// Git branch name restrictions
	if strings.Contains(name, " ") {
		return fmt.Errorf("feature name cannot contain spaces")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("feature name cannot start or end with hyphen")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("feature name cannot contain '..'")
	}
	if strings.ContainsAny(name, "~^:?*[\\") {
		return fmt.Errorf("feature name contains invalid characters")
	}

	return nil
}