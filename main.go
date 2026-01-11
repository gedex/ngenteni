package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

type RepoConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	Interval string `json:"interval"`
	Command  string `json:"command"`
	WorkDir  string `json:"workdir"`
	Timeout  string `json:"timeout,omitempty"`
}

type Config struct {
	Repos []RepoConfig `json:"repos"`
}

type RepoWatcher struct {
	config         RepoConfig
	repoPath       string
	lastCommit     string
	interval       time.Duration
	commandTimeout time.Duration
}

func main() {
	// Define flags
	versionFlag := flag.Bool("version", false, "Print version information")
	versionShort := flag.Bool("v", false, "Print version information (short)")

	flag.Parse()

	// Check version flags
	if *versionFlag || *versionShort {
		fmt.Printf("ngenteni version %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built at: %s\n", date)
		fmt.Printf("  built by: %s\n", builtBy)
		os.Exit(0)
	}

	// Read config file
	configPath := "config.json"
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Loaded config with %d repos", len(config.Repos))

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start watchers
	var wg sync.WaitGroup
	for _, repo := range config.Repos {
		wg.Add(1)
		go func(r RepoConfig) {
			defer wg.Done()
			watcher, err := NewRepoWatcher(r)
			if err != nil {
				log.Printf("[%s] Failed to initialize: %v", r.Name, err)
				return
			}
			watcher.Watch(ctx)
		}(repo)
	}

	log.Println("All watchers started. Press Ctrl+C to stop.")

	// Wait for signal
	<-sigChan
	log.Println("Shutdown signal received, stopping watchers...")
	cancel()

	// Wait for all watchers to finish
	wg.Wait()
	log.Println("All watchers stopped gracefully. Goodbye!")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &config, nil
}

func NewRepoWatcher(config RepoConfig) (*RepoWatcher, error) {
	// Parse interval
	interval, err := time.ParseDuration(config.Interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval: %w", err)
	}

	// Parse timeout (optional)
	var commandTimeout time.Duration
	if config.Timeout != "" {
		commandTimeout, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
		if commandTimeout < 0 {
			return nil, fmt.Errorf("timeout must be positive")
		}
	}

	// Create workdir if not exists
	if err := os.MkdirAll(config.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("creating workdir: %w", err)
	}

	repoPath := filepath.Join(config.WorkDir, config.Name)

	watcher := &RepoWatcher{
		config:         config,
		repoPath:       repoPath,
		interval:       interval,
		commandTimeout: commandTimeout,
	}

	// Initial setup
	if err := watcher.setup(); err != nil {
		return nil, err
	}

	return watcher, nil
}

func (w *RepoWatcher) setup() error {
	// Use background context for initial setup
	ctx := context.Background()

	// Check if repo exists
	if _, err := os.Stat(filepath.Join(w.repoPath, ".git")); os.IsNotExist(err) {
		log.Printf("[%s] Cloning repository...", w.config.Name)
		if err := w.clone(ctx); err != nil {
			return fmt.Errorf("cloning repo: %w", err)
		}
	} else {
		log.Printf("[%s] Repository already exists", w.config.Name)
	}

	// Get initial commit
	commit, err := w.getCurrentCommit(ctx)
	if err != nil {
		return fmt.Errorf("getting initial commit: %w", err)
	}
	w.lastCommit = commit
	log.Printf("[%s] Initial commit: %s", w.config.Name, commit[:8])

	return nil
}

func (w *RepoWatcher) clone(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", w.config.Branch, w.config.URL, w.repoPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (w *RepoWatcher) fetch(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", w.config.Branch)
	cmd.Dir = w.repoPath
	return cmd.Run()
}

func (w *RepoWatcher) getCurrentCommit(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", fmt.Sprintf("origin/%s", w.config.Branch))
	cmd.Dir = w.repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (w *RepoWatcher) Watch(ctx context.Context) {
	log.Printf("[%s] Watching every %s", w.config.Name, w.interval)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.check(ctx); err != nil {
				log.Printf("[%s] Error checking: %v", w.config.Name, err)
			}
		case <-ctx.Done():
			log.Printf("[%s] Stopping watcher", w.config.Name)
			return
		}
	}
}

func (w *RepoWatcher) check(ctx context.Context) error {
	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Fetch latest changes
	if err := w.fetch(ctx); err != nil {
		return fmt.Errorf("fetching: %w", err)
	}

	// Get current commit
	commit, err := w.getCurrentCommit(ctx)
	if err != nil {
		return fmt.Errorf("getting commit: %w", err)
	}

	// Check if changed
	if commit != w.lastCommit {
		log.Printf("[%s] New commit detected: %s -> %s", w.config.Name, w.lastCommit[:8], commit[:8])

		// Run command
		if err := w.runCommand(commit); err != nil {
			if strings.Contains(err.Error(), "timed out") {
				log.Printf("[%s] Command timeout: %v", w.config.Name, err)
			} else {
				log.Printf("[%s] Command failed: %v", w.config.Name, err)
			}
		} else {
			log.Printf("[%s] Command executed successfully", w.config.Name)
		}

		// Update last commit
		w.lastCommit = commit
	}

	return nil
}

func (w *RepoWatcher) runCommand(newCommit string) error {
	if w.config.Command == "" {
		return fmt.Errorf("empty command")
	}

	// Create context for command timeout
	var ctx context.Context
	var cancel context.CancelFunc

	if w.commandTimeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), w.commandTimeout)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	// Execute command through shell with timeout context
	cmd := exec.CommandContext(ctx, "sh", "-c", w.config.Command)
	cmd.Dir = w.repoPath

	// Set environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("REPO_NAME=%s", w.config.Name),
		fmt.Sprintf("REPO_PATH=%s", w.repoPath),
		fmt.Sprintf("REPO_URL=%s", w.config.URL),
		fmt.Sprintf("REPO_BRANCH=%s", w.config.Branch),
		fmt.Sprintf("OLD_COMMIT=%s", w.lastCommit),
		fmt.Sprintf("NEW_COMMIT=%s", newCommit),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run and check for timeout
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("command timed out after %s", w.commandTimeout)
		}
		return err
	}

	return nil
}
