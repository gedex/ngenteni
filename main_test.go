package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper function to create test config file
func createTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Test loadConfig with various scenarios
func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantErr    bool
		wantRepos  int
	}{
		{
			name: "valid single repo",
			configJSON: `{
				"repos": [{
					"name": "test",
					"url": "https://github.com/test/repo.git",
					"branch": "main",
					"interval": "30s",
					"command": "echo test",
					"workdir": "./repos"
				}]
			}`,
			wantErr:   false,
			wantRepos: 1,
		},
		{
			name: "valid multiple repos",
			configJSON: `{
				"repos": [
					{
						"name": "repo1",
						"url": "https://github.com/test/repo1.git",
						"branch": "main",
						"interval": "30s",
						"command": "echo test1",
						"workdir": "./repos"
					},
					{
						"name": "repo2",
						"url": "https://github.com/test/repo2.git",
						"branch": "develop",
						"interval": "1m",
						"command": "echo test2",
						"workdir": "./repos",
						"timeout": "5m"
					}
				]
			}`,
			wantErr:   false,
			wantRepos: 2,
		},
		{
			name:       "invalid JSON",
			configJSON: `{invalid json}`,
			wantErr:    true,
		},
		{
			name:       "empty repos",
			configJSON: `{"repos": []}`,
			wantErr:    false,
			wantRepos:  0,
		},
		{
			name:       "empty file",
			configJSON: ``,
			wantErr:    true,
		},
		{
			name: "valid config with optional timeout",
			configJSON: `{
				"repos": [{
					"name": "test",
					"url": "https://github.com/test/repo.git",
					"branch": "main",
					"interval": "30s",
					"command": "echo test",
					"workdir": "./repos",
					"timeout": "10m"
				}]
			}`,
			wantErr:   false,
			wantRepos: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp config file
			path := createTestConfig(t, tt.configJSON)

			// Load config
			cfg, err := loadConfig(path)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("loadConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check repos count
			if !tt.wantErr && len(cfg.Repos) != tt.wantRepos {
				t.Errorf("got %d repos, want %d", len(cfg.Repos), tt.wantRepos)
			}

			// Verify timeout field is parsed correctly for optional field
			if !tt.wantErr && tt.wantRepos > 0 && strings.Contains(tt.configJSON, "timeout") {
				if cfg.Repos[tt.wantRepos-1].Timeout == "" {
					t.Error("expected timeout field to be parsed, got empty string")
				}
			}
		})
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// Test duration parsing directly (without git operations)
func TestDurationParsing_ValidInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantErr  bool
	}{
		{"30 seconds", "30s", false},
		{"1 minute", "1m", false},
		{"5 minutes", "5m", false},
		{"1 hour", "1h", false},
		{"mixed", "1h30m", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := time.ParseDuration(tt.interval)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDuration() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewRepoWatcher_InvalidInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
	}{
		{"invalid format", "invalid"},
		{"number without unit", "30"},
		{"invalid unit", "30x"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			config := RepoConfig{
				Name:     "test",
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: tt.interval,
				Command:  "echo test",
				WorkDir:  dir,
			}

			_, err := NewRepoWatcher(config)
			if err == nil {
				t.Error("expected error for invalid interval, got nil")
			}
			if !strings.Contains(err.Error(), "invalid interval") {
				t.Errorf("expected 'invalid interval' error, got: %v", err)
			}
		})
	}
}

func TestNewRepoWatcher_TimeoutValidation(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		wantErr     bool
		errContains string
		needsGit    bool // whether this test needs a git repo
	}{
		{"valid timeout", "5m", false, "", true},
		{"valid short timeout", "30s", false, "", true},
		{"valid long timeout", "1h", false, "", true},
		{"empty timeout (no timeout)", "", false, "", true},
		{"invalid format", "invalid", true, "invalid timeout", false},
		{"negative timeout", "-5m", true, "timeout must be positive", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			var repoURL string
			var branch string
			if tt.needsGit {
				// Create a local test git repository
				testRepoDir := filepath.Join(dir, "test-repo")
				if err := os.MkdirAll(testRepoDir, 0755); err != nil {
					t.Fatal(err)
				}

				// Initialize git repo with explicit branch name
				cmd := exec.Command("git", "init", "-b", "main")
				cmd.Dir = testRepoDir
				if err := cmd.Run(); err != nil {
					// Fallback for older git versions that don't support -b
					cmd = exec.Command("git", "init")
					cmd.Dir = testRepoDir
					if err := cmd.Run(); err != nil {
						t.Skipf("git not available: %v", err)
					}
				}

				// Create initial commit
				cmd = exec.Command("git", "config", "user.email", "test@example.com")
				cmd.Dir = testRepoDir
				cmd.Run()
				cmd = exec.Command("git", "config", "user.name", "Test User")
				cmd.Dir = testRepoDir
				cmd.Run()

				// Create a file and commit
				testFile := filepath.Join(testRepoDir, "test.txt")
				if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
					t.Fatal(err)
				}
				cmd = exec.Command("git", "add", "test.txt")
				cmd.Dir = testRepoDir
				cmd.Run()
				cmd = exec.Command("git", "commit", "-m", "initial commit")
				cmd.Dir = testRepoDir
				if err := cmd.Run(); err != nil {
					t.Skipf("git commit failed: %v", err)
				}

				// Get the current branch name
				cmd = exec.Command("git", "branch", "--show-current")
				cmd.Dir = testRepoDir
				output, err := cmd.Output()
				if err != nil {
					branch = "main" // default fallback
				} else {
					branch = strings.TrimSpace(string(output))
				}

				repoURL = testRepoDir
			} else {
				// For validation-only tests, use a fake URL
				repoURL = "https://github.com/test/repo.git"
				branch = "main"
			}

			config := RepoConfig{
				Name:     "test",
				URL:      repoURL,
				Branch:   branch,
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  dir,
				Timeout:  tt.timeout,
			}

			_, err := NewRepoWatcher(config)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewRepoWatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
			}
		})
	}
}

// Test command execution
func TestRunCommand_Success(t *testing.T) {
	w := &RepoWatcher{
		config: RepoConfig{
			Name:    "test",
			Command: "echo test",
		},
		repoPath:       t.TempDir(),
		lastCommit:     "old123",
		commandTimeout: 0,
	}

	err := w.runCommand("new456")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCommand_Failure(t *testing.T) {
	w := &RepoWatcher{
		config: RepoConfig{
			Name:    "test",
			Command: "exit 1",
		},
		repoPath:       t.TempDir(),
		lastCommit:     "old123",
		commandTimeout: 0,
	}

	err := w.runCommand("new456")
	if err == nil {
		t.Error("expected error for failing command, got nil")
	}
}

func TestRunCommand_EmptyCommand(t *testing.T) {
	w := &RepoWatcher{
		config: RepoConfig{
			Name:    "test",
			Command: "",
		},
		repoPath:       t.TempDir(),
		lastCommit:     "old123",
		commandTimeout: 0,
	}

	err := w.runCommand("new456")
	if err == nil {
		t.Error("expected error for empty command, got nil")
	}
	if !strings.Contains(err.Error(), "empty command") {
		t.Errorf("expected 'empty command' error, got: %v", err)
	}
}

func TestRunCommand_Timeout(t *testing.T) {
	tests := []struct {
		name          string
		command       string
		timeout       time.Duration
		expectTimeout bool
	}{
		{
			name:          "command completes before timeout",
			command:       "echo test",
			timeout:       1 * time.Second,
			expectTimeout: false,
		},
		{
			name:          "command exceeds timeout",
			command:       "sleep 5",
			timeout:       100 * time.Millisecond,
			expectTimeout: true,
		},
		{
			name:          "no timeout set",
			command:       "sleep 0.1",
			timeout:       0, // no timeout
			expectTimeout: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &RepoWatcher{
				config: RepoConfig{
					Name:    "test",
					Command: tt.command,
				},
				repoPath:       t.TempDir(),
				lastCommit:     "old123",
				commandTimeout: tt.timeout,
			}

			err := w.runCommand("new456")

			if tt.expectTimeout {
				if err == nil {
					t.Fatal("expected timeout error, got nil")
				}
				if !strings.Contains(err.Error(), "timed out") {
					t.Errorf("expected timeout error, got: %v", err)
				}
			} else {
				if err != nil && tt.command != "exit 1" {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRunCommand_ShellFeatures(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "pipe",
			command: "echo hello | grep hello",
			wantErr: false,
		},
		{
			name:    "logical AND",
			command: "true && echo success",
			wantErr: false,
		},
		{
			name:    "logical OR",
			command: "false || echo fallback",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &RepoWatcher{
				config: RepoConfig{
					Name:    "test",
					Command: tt.command,
				},
				repoPath:       t.TempDir(),
				lastCommit:     "old123",
				commandTimeout: 1 * time.Second,
			}

			err := w.runCommand("new456")

			if (err != nil) != tt.wantErr {
				t.Errorf("runCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test that environment variables are set correctly
func TestRunCommand_EnvironmentVariables(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "check_env.sh")

	// Create a script that checks for environment variables
	script := `#!/bin/sh
test -n "$REPO_NAME" || exit 1
test -n "$REPO_PATH" || exit 1
test -n "$REPO_URL" || exit 1
test -n "$REPO_BRANCH" || exit 1
test -n "$OLD_COMMIT" || exit 1
test -n "$NEW_COMMIT" || exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	w := &RepoWatcher{
		config: RepoConfig{
			Name:    "test-repo",
			Command: scriptPath,
			URL:     "https://github.com/test/repo.git",
			Branch:  "main",
		},
		repoPath:       dir,
		lastCommit:     "oldcommit123",
		commandTimeout: 1 * time.Second,
	}

	err := w.runCommand("newcommit456")
	if err != nil {
		t.Errorf("environment variables not set correctly: %v", err)
	}
}

// Test context cancellation in check()
func TestCheck_ContextCancellation(t *testing.T) {
	// Create a test repo setup (simplified, without actual git)
	dir := t.TempDir()

	w := &RepoWatcher{
		config: RepoConfig{
			Name:    "test",
			Command: "echo test",
			Branch:  "main",
		},
		repoPath:   dir,
		lastCommit: "abc123",
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// check() should return context error
	err := w.check(ctx)
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// Test that working directory is updated when new commits are detected
func TestUpdateWorkingDir(t *testing.T) {
	dir := t.TempDir()

	// Create a test git repository
	testRepoDir := filepath.Join(dir, "test-repo")
	if err := os.MkdirAll(testRepoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = testRepoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = testRepoDir
	cmd.Run()

	// Create initial file and commit
	testFile := filepath.Join(testRepoDir, "version.txt")
	if err := os.WriteFile(testFile, []byte("version 1"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "version.txt")
	cmd.Dir = testRepoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git commit failed: %v", err)
	}

	// Clone to another location
	cloneDir := filepath.Join(dir, "clone")
	cmd = exec.Command("git", "clone", testRepoDir, cloneDir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git clone failed: %v", err)
	}

	// Verify cloned file content
	clonedFile := filepath.Join(cloneDir, "version.txt")
	content, err := os.ReadFile(clonedFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version 1" {
		t.Errorf("expected 'version 1', got '%s'", content)
	}

	// Create second commit in original repo
	if err := os.WriteFile(testFile, []byte("version 2"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "version.txt")
	cmd.Dir = testRepoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "second commit")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Fetch in clone (but don't update working directory yet)
	cmd = exec.Command("git", "fetch", "origin")
	cmd.Dir = cloneDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// File should still be "version 1" (not updated)
	content, _ = os.ReadFile(clonedFile)
	if string(content) != "version 1" {
		t.Errorf("expected file to still be 'version 1' after fetch, got '%s'", content)
	}

	// Now use updateWorkingDir to sync files
	w := &RepoWatcher{
		config: RepoConfig{
			Branch: "main",
		},
		repoPath: cloneDir,
	}

	if err := w.updateWorkingDir(context.Background()); err != nil {
		t.Fatalf("updateWorkingDir failed: %v", err)
	}

	// File should now be "version 2" (updated!)
	content, err = os.ReadFile(clonedFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version 2" {
		t.Errorf("expected file to be 'version 2' after updateWorkingDir, got '%s'", content)
	}
}
