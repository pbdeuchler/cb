package slack

import (
	"reflect"
	"testing"

	"github.com/pbdeuchler/claude-bot/pkg/models"
)

func TestParseStartCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *models.StartCommandParams
		wantErr bool
	}{
		{
			name:  "basic repo",
			input: "start https://github.com/user/repo",
			want: &models.StartCommandParams{
				RepoURL:   "https://github.com/user/repo",
				Branch:    "main",
				UseThread: false,
			},
			wantErr: false,
		},
		{
			name:  "with branch",
			input: "start https://github.com/user/repo feature-branch",
			want: &models.StartCommandParams{
				RepoURL:   "https://github.com/user/repo",
				Branch:    "feature-branch",
				UseThread: false,
			},
			wantErr: false,
		},
		{
			name:  "with thread flag",
			input: "start https://github.com/user/repo --thread",
			want: &models.StartCommandParams{
				RepoURL:   "https://github.com/user/repo",
				Branch:    "main",
				UseThread: true,
			},
			wantErr: false,
		},
		{
			name:  "with branch and thread",
			input: "start https://github.com/user/repo feature-branch --thread",
			want: &models.StartCommandParams{
				RepoURL:   "https://github.com/user/repo",
				Branch:    "feature-branch",
				UseThread: true,
			},
			wantErr: false,
		},
		{
			name:  "gitlab repo",
			input: "start https://gitlab.com/user/repo",
			want: &models.StartCommandParams{
				RepoURL:   "https://gitlab.com/user/repo",
				Branch:    "main",
				UseThread: false,
			},
			wantErr: false,
		},
		{
			name:  "ssh repo",
			input: "start git@github.com:user/repo.git",
			want: &models.StartCommandParams{
				RepoURL:   "git@github.com:user/repo.git",
				Branch:    "main",
				UseThread: false,
			},
			wantErr: false,
		},
		{
			name:    "missing repo",
			input:   "start",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid command",
			input:   "stop https://github.com/user/repo",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid flag",
			input:   "start https://github.com/user/repo --invalid",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid repo url",
			input:   "start not-a-url",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "too many args",
			input:   "start https://github.com/user/repo branch1 branch2",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStartCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStartCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseStartCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseCredentialCommand(t *testing.T) {
	tests := []struct {
		name       string
		input      []string
		wantAction string
		wantType   string
		wantValue  string
		wantErr    bool
	}{
		{
			name:       "set anthropic",
			input:      []string{"set", "anthropic", "sk-ant-api-key"},
			wantAction: "set",
			wantType:   "anthropic",
			wantValue:  "sk-ant-api-key",
			wantErr:    false,
		},
		{
			name:       "set github",
			input:      []string{"set", "github", "ghp_token"},
			wantAction: "set",
			wantType:   "github", 
			wantValue:  "ghp_token",
			wantErr:    false,
		},
		{
			name:       "list",
			input:      []string{"list"},
			wantAction: "list",
			wantType:   "",
			wantValue:  "",
			wantErr:    false,
		},
		{
			name:       "set with spaces in value",
			input:      []string{"set", "anthropic", "sk-ant", "api", "key"},
			wantAction: "set",
			wantType:   "anthropic",
			wantValue:  "sk-ant api key",
			wantErr:    false,
		},
		{
			name:    "empty args",
			input:   []string{},
			wantErr: true,
		},
		{
			name:    "invalid action",
			input:   []string{"invalid"},
			wantErr: true,
		},
		{
			name:    "set missing type",
			input:   []string{"set"},
			wantErr: true,
		},
		{
			name:    "set missing value",
			input:   []string{"set", "anthropic"},
			wantErr: true,
		},
		{
			name:    "invalid credential type",
			input:   []string{"set", "invalid", "value"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAction, gotType, gotValue, err := ParseCredentialCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCredentialCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotAction != tt.wantAction {
				t.Errorf("ParseCredentialCommand() action = %v, want %v", gotAction, tt.wantAction)
			}
			if gotType != tt.wantType {
				t.Errorf("ParseCredentialCommand() type = %v, want %v", gotType, tt.wantType)
			}
			if gotValue != tt.wantValue {
				t.Errorf("ParseCredentialCommand() value = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestIsValidRepoURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"github https", "https://github.com/user/repo", true},
		{"github https with .git", "https://github.com/user/repo.git", true},
		{"github ssh", "git@github.com:user/repo.git", true},
		{"gitlab https", "https://gitlab.com/user/repo", true},
		{"bitbucket https", "https://bitbucket.org/user/repo", true},
		{"generic git hosting", "https://git.example.com/user/repo", true},
		{"empty url", "", false},
		{"invalid url", "not-a-url", false},
		{"http instead of https", "http://github.com/user/repo", false},
		{"missing user/repo", "https://github.com/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidRepoURL(tt.url); got != tt.want {
				t.Errorf("isValidRepoURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidBranchName(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{"main", "main", true},
		{"feature-branch", "feature-branch", true},
		{"feature/branch", "feature/branch", true},
		{"release-1.0", "release-1.0", true},
		{"hotfix_urgent", "hotfix_urgent", true},
		{"empty", "", false},
		{"starts with hyphen", "-branch", false},
		{"contains double dots", "feature..branch", false},
		{"ends with .lock", "branch.lock", false},
		{"starts with slash", "/branch", false},
		{"ends with slash", "branch/", false},
		{"double slashes", "feature//branch", false},
		{"ends with dot", "branch.", false},
		{"contains @{", "branch@{", false},
		{"contains backslash", "branch\\test", false},
		{"contains tilde", "branch~1", false},
		{"contains caret", "branch^1", false},
		{"contains colon", "branch:test", false},
		{"contains asterisk", "branch*", false},
		{"contains question mark", "branch?", false},
		{"contains bracket", "branch[", false},
		{"contains whitespace", "branch name", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidBranchName(tt.branch); got != tt.want {
				t.Errorf("isValidBranchName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommandParser_ParseCommand(t *testing.T) {
	parser := NewCommandParser("UBOT123")

	tests := []struct {
		name        string
		input       string
		wantCommand string
		wantArgs    []string
		wantErr     bool
	}{
		{
			name:        "start command",
			input:       "start https://github.com/user/repo",
			wantCommand: "start",
			wantArgs:    []string{"https://github.com/user/repo"},
			wantErr:     false,
		},
		{
			name:        "stop command",
			input:       "stop",
			wantCommand: "stop",
			wantArgs:    []string{},
			wantErr:     false,
		},
		{
			name:        "help command",
			input:       "help",
			wantCommand: "help",
			wantArgs:    []string{},
			wantErr:     false,
		},
		{
			name:        "credentials command",
			input:       "credentials set anthropic sk-ant-key",
			wantCommand: "credentials",
			wantArgs:    []string{"set", "anthropic", "sk-ant-key"},
			wantErr:     false,
		},
		{
			name:    "invalid command",
			input:   "invalid command",
			wantErr: true,
		},
		{
			name:    "empty command",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCommand, gotArgs, err := parser.ParseCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotCommand != tt.wantCommand {
				t.Errorf("ParseCommand() command = %v, want %v", gotCommand, tt.wantCommand)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("ParseCommand() args = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestExtractMentionedUsers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single mention",
			input: "Hello <@U123456>",
			want:  []string{"U123456"},
		},
		{
			name:  "multiple mentions",
			input: "Hello <@U123456> and <@U789012>",
			want:  []string{"U123456", "U789012"},
		},
		{
			name:  "no mentions",
			input: "Hello world",
			want:  nil,
		},
		{
			name:  "mixed content",
			input: "Hey <@U123456> check out <#C123456|general>",
			want:  []string{"U123456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMentionedUsers(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractMentionedUsers() = %v, want %v", got, tt.want)
			}
		})
	}
}