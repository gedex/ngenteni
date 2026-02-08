package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
			wantErr:    true, // Now validates that repos is not empty
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

// Test config validation
func TestValidateConfig_EmptyRepos(t *testing.T) {
	config := &Config{
		Repos: []RepoConfig{},
	}

	err := validateConfig(config)
	if err == nil {
		t.Error("expected error for empty repos, got nil")
	}
	if !strings.Contains(err.Error(), "no repositories configured") {
		t.Errorf("expected 'no repositories configured' error, got: %v", err)
	}
}

func TestValidateRepoConfig_MissingFields(t *testing.T) {
	tests := []struct {
		name        string
		repo        RepoConfig
		errContains string
	}{
		{
			name:        "missing name",
			repo:        RepoConfig{URL: "url", Branch: "main", Interval: "30s", Command: "cmd", WorkDir: "./"},
			errContains: "name is required",
		},
		{
			name:        "missing url",
			repo:        RepoConfig{Name: "test", Branch: "main", Interval: "30s", Command: "cmd", WorkDir: "./"},
			errContains: "url is required",
		},
		{
			name:        "missing branch",
			repo:        RepoConfig{Name: "test", URL: "url", Interval: "30s", Command: "cmd", WorkDir: "./"},
			errContains: "branch is required",
		},
		{
			name:        "missing interval",
			repo:        RepoConfig{Name: "test", URL: "url", Branch: "main", Command: "cmd", WorkDir: "./"},
			errContains: "interval is required",
		},
		{
			name:        "missing command",
			repo:        RepoConfig{Name: "test", URL: "url", Branch: "main", Interval: "30s", WorkDir: "./"},
			errContains: "command is required",
		},
		{
			name:        "missing workdir",
			repo:        RepoConfig{Name: "test", URL: "url", Branch: "main", Interval: "30s", Command: "cmd"},
			errContains: "workdir is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepoConfig(&tt.repo, 0)
			if err == nil {
				t.Errorf("expected error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
			}
		})
	}
}

func TestValidateRepoConfig_InvalidInterval(t *testing.T) {
	tests := []struct {
		name        string
		interval    string
		errContains string
	}{
		{
			name:        "invalid format",
			interval:    "invalid",
			errContains: "invalid interval",
		},
		{
			name:        "negative interval",
			interval:    "-30s",
			errContains: "interval must be positive",
		},
		{
			name:        "zero interval",
			interval:    "0s",
			errContains: "interval must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := RepoConfig{
				Name:     "test",
				URL:      "url",
				Branch:   "main",
				Interval: tt.interval,
				Command:  "cmd",
				WorkDir:  "./",
			}

			err := validateRepoConfig(&repo, 0)
			if err == nil {
				t.Errorf("expected error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
			}
		})
	}
}

func TestValidateRepoConfig_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		errContains string
	}{
		{
			name:        "invalid format",
			timeout:     "invalid",
			errContains: "invalid timeout",
		},
		{
			name:        "negative timeout",
			timeout:     "-5m",
			errContains: "timeout must be positive",
		},
		{
			name:        "zero timeout",
			timeout:     "0s",
			errContains: "timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := RepoConfig{
				Name:     "test",
				URL:      "url",
				Branch:   "main",
				Interval: "30s",
				Command:  "cmd",
				WorkDir:  "./",
				Timeout:  tt.timeout,
			}

			err := validateRepoConfig(&repo, 0)
			if err == nil {
				t.Errorf("expected error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
			}
		})
	}
}

func TestValidateRepoConfig_Valid(t *testing.T) {
	tests := []struct {
		name string
		repo RepoConfig
	}{
		{
			name: "valid config without timeout",
			repo: RepoConfig{
				Name:     "test",
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
			},
		},
		{
			name: "valid config with timeout",
			repo: RepoConfig{
				Name:     "test",
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "1m",
				Command:  "echo test",
				WorkDir:  "./repos",
				Timeout:  "5m",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepoConfig(&tt.repo, 0)
			if err != nil {
				t.Errorf("unexpected error for valid config: %v", err)
			}
		})
	}
}

func TestLoadConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		configJSON  string
		errContains string
	}{
		{
			name:        "empty repos array",
			configJSON:  `{"repos": []}`,
			errContains: "no repositories configured",
		},
		{
			name: "missing name",
			configJSON: `{
				"repos": [{
					"url": "https://github.com/test/repo.git",
					"branch": "main",
					"interval": "30s",
					"command": "echo test",
					"workdir": "./"
				}]
			}`,
			errContains: "name is required",
		},
		{
			name: "invalid interval",
			configJSON: `{
				"repos": [{
					"name": "test",
					"url": "https://github.com/test/repo.git",
					"branch": "main",
					"interval": "invalid",
					"command": "echo test",
					"workdir": "./"
				}]
			}`,
			errContains: "invalid interval",
		},
		{
			name: "negative timeout",
			configJSON: `{
				"repos": [{
					"name": "test",
					"url": "https://github.com/test/repo.git",
					"branch": "main",
					"interval": "30s",
					"command": "echo test",
					"workdir": "./",
					"timeout": "-5m"
				}]
			}`,
			errContains: "timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTestConfig(t, tt.configJSON)
			_, err := loadConfig(path)

			if err == nil {
				t.Error("expected validation error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
			}
		})
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

// Helper function to create a test git repository
func createTestGitRepo(t *testing.T, dir string) (repoPath string, branch string) {
	t.Helper()
	testRepoDir := filepath.Join(dir, "test-repo")
	if err := os.MkdirAll(testRepoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		cmd = exec.Command("git", "init")
		cmd.Dir = testRepoDir
		if err := cmd.Run(); err != nil {
			t.Skipf("git not available: %v", err)
		}
	}

	// Configure git - handle errors consistently
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git config failed: %v", err)
	}
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git config failed: %v", err)
	}

	// Create initial file and commit
	testFile := filepath.Join(testRepoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = testRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git commit failed: %v", err)
	}

	// Get the current branch name
	cmd = exec.Command("git", "branch", "--show-current")
	cmd.Dir = testRepoDir
	output, err := cmd.Output()
	branch = "main"
	if err == nil {
		branch = strings.TrimSpace(string(output))
	}

	return testRepoDir, branch
}

// Test dry-run mode
func TestDryRun_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	testRepoDir, branch := createTestGitRepo(t, dir)

	// Create config with local repo
	config := &Config{
		Repos: []RepoConfig{
			{
				Name:     "test-repo",
				URL:      testRepoDir,
				Branch:   branch,
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}

	// Run dry-run
	err := dryRun(config)
	if err != nil {
		t.Errorf("dryRun failed with valid config: %v", err)
	}
}

func TestDryRun_InvalidRepo(t *testing.T) {
	config := &Config{
		Repos: []RepoConfig{
			{
				Name:     "invalid-repo",
				URL:      "https://github.com/nonexistent/invalid-repo-12345.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "/tmp/test",
			},
		},
	}

	// Run dry-run - should fail
	err := dryRun(config)
	if err == nil {
		t.Error("expected error for inaccessible repository, got nil")
	}
}

func TestDryRun_InvalidBranch(t *testing.T) {
	dir := t.TempDir()
	testRepoDir, _ := createTestGitRepo(t, dir)

	// Create config with nonexistent branch
	config := &Config{
		Repos: []RepoConfig{
			{
				Name:     "test-repo",
				URL:      testRepoDir,
				Branch:   "nonexistent-branch",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}

	// Run dry-run - should fail
	err := dryRun(config)
	if err == nil {
		t.Error("expected error for nonexistent branch, got nil")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Errorf("expected error about branch, got: %v", err)
	}
}

func TestTestRepoAccess_ValidRepo(t *testing.T) {
	dir := t.TempDir()
	testRepoDir, branch := createTestGitRepo(t, dir)

	// Test repository access
	err := testRepoAccess(testRepoDir, branch)
	if err != nil {
		t.Errorf("testRepoAccess failed for valid repo: %v", err)
	}
}

func TestTestRepoAccess_InvalidRepo(t *testing.T) {
	err := testRepoAccess("https://github.com/nonexistent/invalid-repo-12345.git", "main")
	if err == nil {
		t.Error("expected error for invalid repository, got nil")
	}
}

func TestTestRepoAccess_InvalidBranch(t *testing.T) {
	dir := t.TempDir()
	testRepoDir, _ := createTestGitRepo(t, dir)

	// Test with nonexistent branch
	err := testRepoAccess(testRepoDir, "nonexistent-branch")
	if err == nil {
		t.Error("expected error for nonexistent branch, got nil")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Errorf("expected error about branch, got: %v", err)
	}
}

// Test config reload functionality
func TestWatcherManager_StartWatchers(t *testing.T) {
	dir := t.TempDir()
	testRepoDir, branch := createTestGitRepo(t, dir)

	config := &Config{
		Repos: []RepoConfig{
			{
				Name:     "test-repo",
				URL:      testRepoDir,
				Branch:   branch,
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}

	manager := NewWatcherManager("/tmp/test-config.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	err := manager.StartWatchers(ctx, &wg, config)
	if err != nil {
		t.Fatalf("StartWatchers failed: %v", err)
	}

	// Verify watcher was started
	manager.mu.RLock()
	if len(manager.watchers) != 1 {
		t.Errorf("expected 1 watcher, got %d", len(manager.watchers))
	}
	if _, exists := manager.watchers["test-repo"]; !exists {
		t.Error("expected watcher 'test-repo' to exist")
	}
	manager.mu.RUnlock()

	// Stop watchers
	cancel()
	wg.Wait()
}

func TestWatcherManager_StopWatcher(t *testing.T) {
	manager := NewWatcherManager("/tmp/test-config.json")
	
	// Create a fake watcher
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	watcher := &RepoWatcher{
		config: RepoConfig{Name: "test"},
		cancel: cancel,
	}
	
	manager.mu.Lock()
	manager.watchers["test"] = watcher
	manager.mu.Unlock()

	// Stop the watcher
	manager.StopWatcher("test")

	// Verify it was removed
	manager.mu.RLock()
	if len(manager.watchers) != 0 {
		t.Errorf("expected 0 watchers, got %d", len(manager.watchers))
	}
	manager.mu.RUnlock()
}

func TestWatcherManager_ReloadConfig_AddRepo(t *testing.T) {
	dir := t.TempDir()
	testRepoDir1, branch1 := createTestGitRepo(t, filepath.Join(dir, "repo1"))
	testRepoDir2, branch2 := createTestGitRepo(t, filepath.Join(dir, "repo2"))

	// Create initial config with one repo
	configPath := filepath.Join(dir, "config.json")
	config1 := &Config{
		Repos: []RepoConfig{
			{
				Name:     "repo1",
				URL:      testRepoDir1,
				Branch:   branch1,
				Interval: "30s",
				Command:  "echo test1",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}
	
	data, _ := json.Marshal(config1)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	manager := NewWatcherManager(configPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	
	// Start initial watchers
	if err := manager.StartWatchers(ctx, &wg, config1); err != nil {
		t.Fatalf("StartWatchers failed: %v", err)
	}

	// Verify initial state
	manager.mu.RLock()
	if len(manager.watchers) != 1 {
		t.Errorf("expected 1 watcher initially, got %d", len(manager.watchers))
	}
	manager.mu.RUnlock()

	// Update config to add a second repo
	config2 := &Config{
		Repos: []RepoConfig{
			{
				Name:     "repo1",
				URL:      testRepoDir1,
				Branch:   branch1,
				Interval: "30s",
				Command:  "echo test1",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
			{
				Name:     "repo2",
				URL:      testRepoDir2,
				Branch:   branch2,
				Interval: "1m",
				Command:  "echo test2",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}
	
	data, _ = json.Marshal(config2)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Reload config
	if err := manager.ReloadConfig(ctx, &wg); err != nil {
		t.Fatalf("ReloadConfig failed: %v", err)
	}

	// Verify both watchers exist
	manager.mu.RLock()
	if len(manager.watchers) != 2 {
		t.Errorf("expected 2 watchers after reload, got %d", len(manager.watchers))
	}
	if _, exists := manager.watchers["repo1"]; !exists {
		t.Error("expected watcher 'repo1' to exist")
	}
	if _, exists := manager.watchers["repo2"]; !exists {
		t.Error("expected watcher 'repo2' to exist")
	}
	manager.mu.RUnlock()

	// Cleanup
	cancel()
	wg.Wait()
}

func TestWatcherManager_ReloadConfig_RemoveRepo(t *testing.T) {
	dir := t.TempDir()
	testRepoDir1, branch1 := createTestGitRepo(t, filepath.Join(dir, "repo1"))
	testRepoDir2, branch2 := createTestGitRepo(t, filepath.Join(dir, "repo2"))

	// Create initial config with two repos
	configPath := filepath.Join(dir, "config.json")
	config1 := &Config{
		Repos: []RepoConfig{
			{
				Name:     "repo1",
				URL:      testRepoDir1,
				Branch:   branch1,
				Interval: "30s",
				Command:  "echo test1",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
			{
				Name:     "repo2",
				URL:      testRepoDir2,
				Branch:   branch2,
				Interval: "1m",
				Command:  "echo test2",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}
	
	data, _ := json.Marshal(config1)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	manager := NewWatcherManager(configPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	
	// Start initial watchers
	if err := manager.StartWatchers(ctx, &wg, config1); err != nil {
		t.Fatalf("StartWatchers failed: %v", err)
	}

	// Verify initial state
	manager.mu.RLock()
	if len(manager.watchers) != 2 {
		t.Errorf("expected 2 watchers initially, got %d", len(manager.watchers))
	}
	manager.mu.RUnlock()

	// Update config to remove repo2
	config2 := &Config{
		Repos: []RepoConfig{
			{
				Name:     "repo1",
				URL:      testRepoDir1,
				Branch:   branch1,
				Interval: "30s",
				Command:  "echo test1",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}
	
	data, _ = json.Marshal(config2)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Reload config
	if err := manager.ReloadConfig(ctx, &wg); err != nil {
		t.Fatalf("ReloadConfig failed: %v", err)
	}

	// Verify only repo1 watcher exists
	manager.mu.RLock()
	if len(manager.watchers) != 1 {
		t.Errorf("expected 1 watcher after reload, got %d", len(manager.watchers))
	}
	if _, exists := manager.watchers["repo1"]; !exists {
		t.Error("expected watcher 'repo1' to exist")
	}
	if _, exists := manager.watchers["repo2"]; exists {
		t.Error("expected watcher 'repo2' to be removed")
	}
	manager.mu.RUnlock()

	// Cleanup
	cancel()
	wg.Wait()
}

func TestWatcherManager_ReloadConfig_UpdateRepo(t *testing.T) {
	dir := t.TempDir()
	testRepoDir, branch := createTestGitRepo(t, dir)

	// Create initial config
	configPath := filepath.Join(dir, "config.json")
	config1 := &Config{
		Repos: []RepoConfig{
			{
				Name:     "test-repo",
				URL:      testRepoDir,
				Branch:   branch,
				Interval: "30s",
				Command:  "echo test1",
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}
	
	data, _ := json.Marshal(config1)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	manager := NewWatcherManager(configPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	
	// Start initial watchers
	if err := manager.StartWatchers(ctx, &wg, config1); err != nil {
		t.Fatalf("StartWatchers failed: %v", err)
	}

	// Get initial watcher reference
	manager.mu.RLock()
	initialWatcher := manager.watchers["test-repo"]
	initialCommand := initialWatcher.config.Command
	manager.mu.RUnlock()

	if initialCommand != "echo test1" {
		t.Errorf("expected initial command 'echo test1', got '%s'", initialCommand)
	}

	// Update config with different command
	config2 := &Config{
		Repos: []RepoConfig{
			{
				Name:     "test-repo",
				URL:      testRepoDir,
				Branch:   branch,
				Interval: "30s",
				Command:  "echo test2",  // Changed command
				WorkDir:  filepath.Join(dir, "workdir"),
			},
		},
	}
	
	data, _ = json.Marshal(config2)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Reload config
	if err := manager.ReloadConfig(ctx, &wg); err != nil {
		t.Fatalf("ReloadConfig failed: %v", err)
	}

	// Verify watcher was restarted with new config
	manager.mu.RLock()
	updatedWatcher := manager.watchers["test-repo"]
	updatedCommand := updatedWatcher.config.Command
	manager.mu.RUnlock()

	if updatedCommand != "echo test2" {
		t.Errorf("expected updated command 'echo test2', got '%s'", updatedCommand)
	}

	// Cleanup
	cancel()
	wg.Wait()
}

func TestConfigChanged(t *testing.T) {
	tests := []struct {
		name     string
		old      RepoConfig
		new      RepoConfig
		expected bool
	}{
		{
			name: "no change",
			old: RepoConfig{
				Name:     "test",
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
				Timeout:  "5m",
			},
			new: RepoConfig{
				Name:     "test",
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
				Timeout:  "5m",
			},
			expected: false,
		},
		{
			name: "URL changed",
			old: RepoConfig{
				URL:      "https://github.com/test/repo1.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
			},
			new: RepoConfig{
				URL:      "https://github.com/test/repo2.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
			},
			expected: true,
		},
		{
			name: "Branch changed",
			old: RepoConfig{
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
			},
			new: RepoConfig{
				URL:      "https://github.com/test/repo.git",
				Branch:   "develop",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
			},
			expected: true,
		},
		{
			name: "Command changed",
			old: RepoConfig{
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test1",
				WorkDir:  "./repos",
			},
			new: RepoConfig{
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test2",
				WorkDir:  "./repos",
			},
			expected: true,
		},
		{
			name: "Timeout changed",
			old: RepoConfig{
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
				Timeout:  "5m",
			},
			new: RepoConfig{
				URL:      "https://github.com/test/repo.git",
				Branch:   "main",
				Interval: "30s",
				Command:  "echo test",
				WorkDir:  "./repos",
				Timeout:  "10m",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := configChanged(tt.old, tt.new)
			if result != tt.expected {
				t.Errorf("configChanged() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

