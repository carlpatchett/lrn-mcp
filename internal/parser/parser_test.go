package parser

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "kubectl with quotes and labels",
			input:    `kubectl get pods -n prod -l "app=frontend,tier=web"`,
			expected: []string{"kubectl", "get", "pods", "-n", "prod", "-l", "app=frontend,tier=web"},
		},
		{
			name:     "git commit with empty string argument",
			input:    `git commit -m ""`,
			expected: []string{"git", "commit", "-m", ""},
		},
		{
			name:     "single quotes literal preserving backslashes",
			input:    `git commit -m 'Fix: \n is literal'`,
			expected: []string{"git", "commit", "-m", `Fix: \n is literal`},
		},
		{
			name:     "jj describe with revision",
			input:    `jj describe -m "fix(auth): correct token refresh" -r @-`,
			expected: []string{"jj", "describe", "-m", "fix(auth): correct token refresh", "-r", "@-"},
		},
		{
			name:     "single dash positional argument",
			input:    `git checkout -`,
			expected: []string{"git", "checkout", "-"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokens := Tokenize(c.input)
			if len(tokens) != len(c.expected) {
				t.Fatalf("expected %d tokens, got %d: %+v", len(c.expected), len(tokens), tokens)
			}
			for i := range tokens {
				if tokens[i] != c.expected[i] {
					t.Errorf("token %d mismatch: expected %q, got %q", i, c.expected[i], tokens[i])
				}
			}
		})
	}
}

func TestParseCommands(t *testing.T) {
	tests := []struct {
		name         string
		rawCmd       string
		expectedTool string
		expectedSub  []string
		expectedSafe string
	}{
		{
			name:         "kubectl get pods",
			rawCmd:       "kubectl get pods -n prod -l app=web -o wide",
			expectedTool: "kubectl",
			expectedSub:  []string{"get", "pods"},
			expectedSafe: SafetyReadOnly,
		},
		{
			name:         "kubectl apply with manifest flag",
			rawCmd:       "kubectl apply -f manifest.yaml",
			expectedTool: "kubectl",
			expectedSub:  []string{"apply"},
			expectedSafe: SafetyMutating,
		},
		{
			name:         "kubectl run container",
			rawCmd:       "kubectl run my-pod --image=nginx",
			expectedTool: "kubectl",
			expectedSub:  []string{"run"},
			expectedSafe: SafetyMutating,
		},
		{
			name:         "kubectl delete namespace",
			rawCmd:       "kubectl delete ns dev",
			expectedTool: "kubectl",
			expectedSub:  []string{"delete"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "git commit with amend",
			rawCmd:       "git commit --amend --no-edit",
			expectedTool: "git",
			expectedSub:  []string{"commit"},
			expectedSafe: SafetyMutating,
		},
		{
			name:         "git push force",
			rawCmd:       "git push origin main --force",
			expectedTool: "git",
			expectedSub:  []string{"push"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "git push force-with-lease is mutating",
			rawCmd:       "git push origin main --force-with-lease",
			expectedTool: "git",
			expectedSub:  []string{"push"},
			expectedSafe: SafetyMutating,
		},
		{
			name:         "git reset hard is destructive",
			rawCmd:       "git reset --hard HEAD~1",
			expectedTool: "git",
			expectedSub:  []string{"reset"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "git clean -f is destructive",
			rawCmd:       "git clean -fd",
			expectedTool: "git",
			expectedSub:  []string{"clean"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "git branch -D is destructive",
			rawCmd:       "git branch -D stale-feature",
			expectedTool: "git",
			expectedSub:  []string{"branch"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "jj squash",
			rawCmd:       "jj squash -r my-feature",
			expectedTool: "jj",
			expectedSub:  []string{"squash"},
			expectedSafe: SafetyMutating,
		},
		{
			name:         "jj abandon is destructive",
			rawCmd:       "jj abandon @-",
			expectedTool: "jj",
			expectedSub:  []string{"abandon"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "docker run detached",
			rawCmd:       "docker run -d --rm -p 8080:80 nginx:latest",
			expectedTool: "docker",
			expectedSub:  []string{"run"},
			expectedSafe: SafetyMutating,
		},
		{
			name:         "docker compose down is destructive",
			rawCmd:       "docker compose down",
			expectedTool: "docker",
			expectedSub:  []string{"compose", "down"},
			expectedSafe: SafetyDestructive,
		},
		{
			name:         "terraform state rm is destructive",
			rawCmd:       "terraform state rm aws_s3_bucket.main",
			expectedTool: "terraform",
			expectedSub:  []string{"state", "rm"},
			expectedSafe: SafetyDestructive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := Parse(tt.rawCmd, nil)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if cmd.BaseTool != tt.expectedTool {
				t.Errorf("expected tool %s, got %s", tt.expectedTool, cmd.BaseTool)
			}
			if len(cmd.Subcommands) != len(tt.expectedSub) {
				t.Errorf("expected subcommands %+v, got %+v", tt.expectedSub, cmd.Subcommands)
			}
			if cmd.SafetyLevel != tt.expectedSafe {
				t.Errorf("expected safety %s, got %s", tt.expectedSafe, cmd.SafetyLevel)
			}
		})
	}
}

func TestQuoteAwarePipeline(t *testing.T) {
	cmd, err := Parse(`kubectl get pods -l 'app | web' | grep Running`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmd.PipedCommands) != 1 || cmd.PipedCommands[0] != "grep Running" {
		t.Errorf("unexpected piped commands: %+v", cmd.PipedCommands)
	}
}
