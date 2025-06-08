package slack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pbdeuchler/claude-bot/pkg/models"
)

// CommandParser parses Slack messages and extracts commands
type CommandParser struct {
	botUserID string
}

// NewCommandParser creates a new command parser
func NewCommandParser(botUserID string) *CommandParser {
	return &CommandParser{
		botUserID: botUserID,
	}
}

// ParseStartCommand parses a start command from Slack message text
// Format: start <repo-url> [branch] [--thread]
func ParseStartCommand(text string) (*models.StartCommandParams, error) {
	// Remove bot mention and normalize whitespace
	text = cleanMessageText(text)
	
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			"usage: @cb start <repo-url> [branch] [--thread]", nil)
	}

	if strings.ToLower(parts[0]) != "start" {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			"expected 'start' command", nil)
	}

	params := &models.StartCommandParams{
		RepoURL:   parts[1],
		Branch:    "main",
		UseThread: false,
	}

	// Validate repository URL
	if !isValidRepoURL(params.RepoURL) {
		return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			"invalid repository URL", nil)
	}

	// Parse additional arguments
	for i := 2; i < len(parts); i++ {
		arg := parts[i]
		if arg == "--thread" {
			params.UseThread = true
		} else if strings.HasPrefix(arg, "--") {
			return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
				fmt.Sprintf("unknown flag: %s", arg), nil)
		} else if params.Branch == "main" { // Only set branch if it's still default
			if !isValidBranchName(arg) {
				return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
					"invalid branch name", nil)
			}
			params.Branch = arg
		} else {
			return nil, models.NewCBError(models.ErrCodeInvalidCommand, 
				"too many arguments", nil)
		}
	}

	return params, nil
}

// ParseCommand identifies and parses any command from a Slack message
func (cp *CommandParser) ParseCommand(text string) (string, []string, error) {
	// Clean the message text
	text = cleanMessageText(text)
	
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			"empty command", nil)
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	// Validate command
	validCommands := []string{"start", "stop", "status", "help", "list", "credentials"}
	isValid := false
	for _, valid := range validCommands {
		if command == valid {
			isValid = true
			break
		}
	}

	if !isValid {
		return "", nil, models.NewCBError(models.ErrCodeInvalidCommand, 
			fmt.Sprintf("unknown command: %s. Try 'help' for available commands", command), nil)
	}

	return command, args, nil
}

// ParseCredentialCommand parses credential-related commands
// Format: credentials set <type> <value>
// Format: credentials list
func ParseCredentialCommand(args []string) (string, string, string, error) {
	if len(args) == 0 {
		return "", "", "", models.NewCBError(models.ErrCodeInvalidCommand, 
			"usage: credentials <set|list> [type] [value]", nil)
	}

	action := strings.ToLower(args[0])
	
	switch action {
	case "list":
		return action, "", "", nil
	case "set":
		if len(args) < 3 {
			return "", "", "", models.NewCBError(models.ErrCodeInvalidCommand, 
				"usage: credentials set <type> <value>", nil)
		}
		credType := strings.ToLower(args[1])
		value := strings.Join(args[2:], " ") // Allow spaces in values
		
		// Validate credential type
		if credType != models.CredentialTypeAnthropic && credType != models.CredentialTypeGitHub {
			return "", "", "", models.NewCBError(models.ErrCodeInvalidCommand, 
				"credential type must be 'anthropic' or 'github'", nil)
		}
		
		if value == "" {
			return "", "", "", models.NewCBError(models.ErrCodeInvalidCommand, 
				"credential value cannot be empty", nil)
		}
		
		return action, credType, value, nil
	default:
		return "", "", "", models.NewCBError(models.ErrCodeInvalidCommand, 
			"credential action must be 'set' or 'list'", nil)
	}
}

// IsDirectMention checks if the message is a direct mention of the bot
func (cp *CommandParser) IsDirectMention(text string) bool {
	mentionPattern := fmt.Sprintf(`<@%s>`, cp.botUserID)
	matched, _ := regexp.MatchString(mentionPattern, text)
	return matched
}

// IsBotMessage checks if a message is from the bot itself
func (cp *CommandParser) IsBotMessage(userID string) bool {
	return userID == cp.botUserID
}

// ExtractMentionedUsers extracts all user IDs mentioned in a message
func ExtractMentionedUsers(text string) []string {
	mentionRegex := regexp.MustCompile(`<@([A-Z0-9]+)>`)
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	
	var users []string
	for _, match := range matches {
		if len(match) > 1 {
			users = append(users, match[1])
		}
	}
	
	return users
}

// ExtractChannelMentions extracts all channel IDs mentioned in a message
func ExtractChannelMentions(text string) []string {
	channelRegex := regexp.MustCompile(`<#([A-Z0-9]+)\|([^>]+)>`)
	matches := channelRegex.FindAllStringSubmatch(text, -1)
	
	var channels []string
	for _, match := range matches {
		if len(match) > 1 {
			channels = append(channels, match[1])
		}
	}
	
	return channels
}

// Helper functions

// cleanMessageText removes bot mentions and normalizes whitespace
func cleanMessageText(text string) string {
	// Remove bot mentions
	mentionRegex := regexp.MustCompile(`<@[A-Z0-9]+>`)
	text = mentionRegex.ReplaceAllString(text, "")
	
	// Normalize whitespace
	text = strings.TrimSpace(text)
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	
	return text
}

// isValidRepoURL validates a repository URL
func isValidRepoURL(url string) bool {
	if url == "" {
		return false
	}
	
	// Check for common Git hosting patterns
	patterns := []string{
		`^https://github\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:\.git)?/?$`,
		`^git@github\.com:[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:\.git)?$`,
		`^https://gitlab\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:\.git)?/?$`,
		`^https://bitbucket\.org/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:\.git)?/?$`,
		`^https://[a-zA-Z0-9.-]+/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:\.git)?/?$`, // Generic Git hosting
	}
	
	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, url); matched {
			return true
		}
	}
	
	return false
}

// isValidBranchName validates a Git branch name
func isValidBranchName(name string) bool {
	if name == "" {
		return false
	}
	
	// Basic Git branch name validation
	// Cannot start with -, cannot contain .., cannot end with .lock, etc.
	invalidPatterns := []string{
		`^-`,        // Cannot start with hyphen
		`\.\.`,      // Cannot contain double dots
		`\.lock$`,   // Cannot end with .lock
		`^/`,        // Cannot start with slash
		`/$`,        // Cannot end with slash
		`//`,        // Cannot contain double slashes
		`\.$`,       // Cannot end with dot
		`@\{`,       // Cannot contain @{
		`\\`,        // Cannot contain backslash
		`\s`,        // Cannot contain whitespace
		`[~^:]`,     // Cannot contain ~, ^, or :
		`\*`,        // Cannot contain *
		`\?`,        // Cannot contain ?
		`\[`,        // Cannot contain [
	}
	
	for _, pattern := range invalidPatterns {
		if matched, _ := regexp.MatchString(pattern, name); matched {
			return false
		}
	}
	
	// Must contain at least one valid character
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9/_.-]+$`, name); !matched {
		return false
	}
	
	return true
}

// FormatHelpMessage returns a formatted help message
func FormatHelpMessage() string {
	return "*Claude Bot Commands:*\n\n" +
		"• `start <repo-url> [branch] [--thread]` - Start a new coding session\n" +
		"  • `repo-url`: GitHub, GitLab, or other Git repository URL\n" +
		"  • `branch`: Branch name (defaults to 'main')\n" +
		"  • `--thread`: Start session in a thread (optional)\n\n" +
		"• `stop` - End the current session in this channel/thread\n\n" +
		"• `status` - Show current session status\n\n" +
		"• `list` - List your active sessions\n\n" +
		"• `credentials set <type> <value>` - Set API credentials\n" +
		"  • `type`: 'anthropic' or 'github'\n" +
		"  • `value`: Your API key/token\n\n" +
		"• `credentials list` - List your stored credential types\n\n" +
		"• `help` - Show this help message\n\n" +
		"*Examples:*\n" +
		"• `@cb start https://github.com/user/repo`\n" +
		"• `@cb start https://github.com/user/repo feature-branch --thread`\n" +
		"• `@cb credentials set anthropic sk-ant-...`\n" +
		"• `@cb stop`\n\n" +
		"*Note:* Sessions cannot be started in #general channel."
}

// FormatErrorMessage formats an error for Slack display
func FormatErrorMessage(err error) string {
	if cbErr, ok := err.(*models.CBError); ok {
		return fmt.Sprintf(":x: *Error (%s):* %s", cbErr.Code, cbErr.Message)
	}
	return fmt.Sprintf(":x: *Error:* %s", err.Error())
}

// FormatSuccessMessage formats a success message for Slack display
func FormatSuccessMessage(message string) string {
	return fmt.Sprintf(":white_check_mark: %s", message)
}

// FormatSessionInfo formats session information for Slack display
func FormatSessionInfo(info map[string]interface{}) string {
	var parts []string
	
	if sessionID, ok := info["session_id"].(string); ok {
		parts = append(parts, fmt.Sprintf("*Session ID:* %s", sessionID))
	}
	
	if status, ok := info["status"].(string); ok {
		statusEmoji := ":white_circle:"
		switch status {
		case models.SessionStatusActive:
			statusEmoji = ":green_circle:"
		case models.SessionStatusEnding:
			statusEmoji = ":yellow_circle:"
		case models.SessionStatusEnded:
			statusEmoji = ":red_circle:"
		case models.SessionStatusError:
			statusEmoji = ":red_circle:"
		}
		parts = append(parts, fmt.Sprintf("*Status:* %s %s", statusEmoji, status))
	}
	
	if repoURL, ok := info["repo_url"].(string); ok {
		parts = append(parts, fmt.Sprintf("*Repository:* %s", repoURL))
	}
	
	if branch, ok := info["branch"].(string); ok {
		parts = append(parts, fmt.Sprintf("*Branch:* %s", branch))
	}
	
	if claudeStatus, ok := info["claude_status"].(string); ok {
		parts = append(parts, fmt.Sprintf("*Claude Status:* %s", claudeStatus))
	}
	
	return strings.Join(parts, "\n")
}