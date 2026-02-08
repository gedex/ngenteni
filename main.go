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

	"github.com/fsnotify/fsnotify"
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
	cancel         context.CancelFunc
	mu             sync.Mutex
}

type WatcherManager struct {
	watchers   map[string]*RepoWatcher
	mu         sync.RWMutex
	configPath string
}

func NewWatcherManager(configPath string) *WatcherManager {
	return &WatcherManager{
		watchers:   make(map[string]*RepoWatcher),
		configPath: configPath,
	}
}

// StartWatchers initializes and starts watchers for all repositories in the config
func (wm *WatcherManager) StartWatchers(ctx context.Context, wg *sync.WaitGroup, config *Config) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	for _, repo := range config.Repos {
		if _, exists := wm.watchers[repo.Name]; exists {
			continue // Skip if already running
		}

		watcher, err := NewRepoWatcher(repo)
		if err != nil {
			log.Printf("[%s] Failed to initialize: %v", repo.Name, err)
			continue
		}

		// Create a context for this watcher
		watcherCtx, cancel := context.WithCancel(ctx)
		watcher.cancel = cancel

		wm.watchers[repo.Name] = watcher

		wg.Add(1)
		go func(w *RepoWatcher) {
			defer wg.Done()
			w.Watch(watcherCtx)
		}(watcher)
	}

	return nil
}

// StopWatcher stops a specific watcher by name
func (wm *WatcherManager) StopWatcher(name string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if watcher, exists := wm.watchers[name]; exists {
		if watcher.cancel != nil {
			watcher.cancel()
		}
		delete(wm.watchers, name)
		log.Printf("[%s] Watcher stopped", name)
	}
}

// ReloadConfig reloads the configuration and updates watchers
func (wm *WatcherManager) ReloadConfig(ctx context.Context, wg *sync.WaitGroup) error {
	// Load new config
	newConfig, err := loadConfig(wm.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log.Printf("Config reloaded with %d repos", len(newConfig.Repos))

	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Build map of new repos
	newRepos := make(map[string]RepoConfig)
	for _, repo := range newConfig.Repos {
		newRepos[repo.Name] = repo
	}

	// Stop watchers for removed repos
	for name, watcher := range wm.watchers {
		if _, exists := newRepos[name]; !exists {
			log.Printf("[%s] Repository removed from config, stopping watcher", name)
			if watcher.cancel != nil {
				watcher.cancel()
			}
			delete(wm.watchers, name)
		}
	}

	// Update or start watchers
	for name, newRepo := range newRepos {
		if watcher, exists := wm.watchers[name]; exists {
			// Check if config changed
			if configChanged(watcher.config, newRepo) {
				log.Printf("[%s] Configuration changed, restarting watcher", name)
				// Stop old watcher
				if watcher.cancel != nil {
					watcher.cancel()
				}
				delete(wm.watchers, name)

				// Start new watcher
				newWatcher, err := NewRepoWatcher(newRepo)
				if err != nil {
					log.Printf("[%s] Failed to initialize after config change: %v", name, err)
					continue
				}

				watcherCtx, cancel := context.WithCancel(ctx)
				newWatcher.cancel = cancel
				wm.watchers[name] = newWatcher

				wg.Add(1)
				go func(w *RepoWatcher) {
					defer wg.Done()
					w.Watch(watcherCtx)
				}(newWatcher)
			} else {
				log.Printf("[%s] Configuration unchanged, keeping watcher", name)
			}
		} else {
			// New repo, start watcher
			log.Printf("[%s] New repository detected, starting watcher", name)
			newWatcher, err := NewRepoWatcher(newRepo)
			if err != nil {
				log.Printf("[%s] Failed to initialize: %v", name, err)
				continue
			}

			watcherCtx, cancel := context.WithCancel(ctx)
			newWatcher.cancel = cancel
			wm.watchers[name] = newWatcher

			wg.Add(1)
			go func(w *RepoWatcher) {
				defer wg.Done()
				w.Watch(watcherCtx)
			}(newWatcher)
		}
	}

	return nil
}

// WatchConfigFile watches the config file for changes and reloads
func (wm *WatcherManager) WatchConfigFile(ctx context.Context, wg *sync.WaitGroup) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating file watcher: %w", err)
	}
	defer watcher.Close()

	// Watch the config file
	if err := watcher.Add(wm.configPath); err != nil {
		return fmt.Errorf("watching config file: %w", err)
	}

	log.Printf("Watching config file: %s", wm.configPath)

	// Debounce timer to handle rapid successive writes
	var debounceTimer *time.Timer
	debounceDuration := 500 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Handle file events (Write or Create)
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Reset or create debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}

				debounceTimer = time.AfterFunc(debounceDuration, func() {
					log.Println("Config file changed, reloading...")
					if err := wm.ReloadConfig(ctx, wg); err != nil {
						log.Printf("Failed to reload config: %v", err)
					} else {
						log.Println("Config reloaded successfully")
					}
				})
			}

			// Handle file removal and recreation (common with some editors)
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				log.Println("Config file removed or renamed, re-watching...")
				// Re-add the watch (some editors remove and recreate files)
				time.Sleep(100 * time.Millisecond) // Small delay for file to be recreated
				_ = watcher.Remove(wm.configPath)
				if err := watcher.Add(wm.configPath); err != nil {
					log.Printf("Failed to re-watch config file: %v", err)
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Config file watcher error: %v", err)
		}
	}
}

// configChanged checks if two RepoConfig structs differ
func configChanged(old, new RepoConfig) bool {
	return old.URL != new.URL ||
		old.Branch != new.Branch ||
		old.Interval != new.Interval ||
		old.Command != new.Command ||
		old.WorkDir != new.WorkDir ||
		old.Timeout != new.Timeout
}

func main() {
	// Define flags
	versionFlag := flag.Bool("version", false, "Print version information")
	versionShort := flag.Bool("v", false, "Print version information (short)")
	dryRunFlag := flag.Bool("dry-run", false, "Validate configuration and test repository access without running watchers")

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

	// Handle dry-run mode
	if *dryRunFlag {
		log.Println("Running in dry-run mode - no commands will be executed")
		if err := dryRun(config); err != nil {
			log.Fatalf("Dry-run validation failed: %v", err)
		}
		log.Println("Dry-run completed successfully!")
		return
	}

	// Get absolute path for config file
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		log.Fatalf("Failed to get absolute config path: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create watcher manager
	manager := NewWatcherManager(absConfigPath)

	// Start initial watchers
	var wg sync.WaitGroup
	if err := manager.StartWatchers(ctx, &wg, config); err != nil {
		log.Fatalf("Failed to start watchers: %v", err)
	}

	// Start config file watcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := manager.WatchConfigFile(ctx, &wg); err != nil && err != context.Canceled {
			log.Printf("Config file watcher error: %v", err)
		}
	}()

	log.Println("All watchers started. Config file is being watched for changes. Press Ctrl+C to stop.")

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

	// Validate all repo configurations
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func validateConfig(config *Config) error {
	if len(config.Repos) == 0 {
		return fmt.Errorf("no repositories configured")
	}

	for i, repo := range config.Repos {
		if err := validateRepoConfig(&repo, i); err != nil {
			return err
		}
	}

	return nil
}

func validateRepoConfig(repo *RepoConfig, index int) error {
	// Helper to create error messages with repo context
	errPrefix := func() string {
		if repo.Name != "" {
			return fmt.Sprintf("repo '%s'", repo.Name)
		}
		return fmt.Sprintf("repo at index %d", index)
	}

	// Validate required fields
	if repo.Name == "" {
		return fmt.Errorf("%s: name is required", errPrefix())
	}
	if repo.URL == "" {
		return fmt.Errorf("%s: url is required", errPrefix())
	}
	if repo.Branch == "" {
		return fmt.Errorf("%s: branch is required", errPrefix())
	}
	if repo.Interval == "" {
		return fmt.Errorf("%s: interval is required", errPrefix())
	}
	if repo.Command == "" {
		return fmt.Errorf("%s: command is required", errPrefix())
	}
	if repo.WorkDir == "" {
		return fmt.Errorf("%s: workdir is required", errPrefix())
	}

	// Validate interval format
	interval, err := time.ParseDuration(repo.Interval)
	if err != nil {
		return fmt.Errorf("%s: invalid interval '%s': %w", errPrefix(), repo.Interval, err)
	}
	if interval <= 0 {
		return fmt.Errorf("%s: interval must be positive, got %s", errPrefix(), repo.Interval)
	}

	// Validate timeout format (if provided)
	if repo.Timeout != "" {
		timeout, err := time.ParseDuration(repo.Timeout)
		if err != nil {
			return fmt.Errorf("%s: invalid timeout '%s': %w", errPrefix(), repo.Timeout, err)
		}
		if timeout <= 0 {
			return fmt.Errorf("%s: timeout must be positive, got %s", errPrefix(), repo.Timeout)
		}
	}

	return nil
}

// dryRun validates configuration and tests repository accessibility without executing commands
func dryRun(config *Config) error {
	log.Println("Validating configuration...")
	
	// Configuration is already validated by loadConfig, but let's report it
	log.Printf("✓ Configuration is valid (%d repositories configured)", len(config.Repos))
	
	// Test each repository
	for i, repo := range config.Repos {
		log.Printf("\n[%d/%d] Checking repository: %s", i+1, len(config.Repos), repo.Name)
		
		// Validate repository URL accessibility
		log.Printf("  Testing repository accessibility: %s", repo.URL)
		if err := testRepoAccess(repo.URL, repo.Branch); err != nil {
			log.Printf("  ✗ Repository access failed: %v", err)
			return fmt.Errorf("repo '%s': %w", repo.Name, err)
		}
		log.Printf("  ✓ Repository is accessible")
		
		// Parse and display interval
		interval, _ := time.ParseDuration(repo.Interval)
		log.Printf("  ✓ Polling interval: %s", interval)
		
		// Parse and display timeout if set
		if repo.Timeout != "" {
			timeout, _ := time.ParseDuration(repo.Timeout)
			log.Printf("  ✓ Command timeout: %s", timeout)
		} else {
			log.Printf("  ℹ Command timeout: none (unlimited)")
		}
		
		// Display work directory
		log.Printf("  ✓ Work directory: %s", repo.WorkDir)
		
		// Display command (but don't execute it)
		log.Printf("  ✓ Command configured: %s", repo.Command)
		log.Printf("  ℹ Command would execute when new commits are detected")
	}
	
	log.Println("\n✓ All repositories validated successfully")
	log.Println("✓ Configuration is ready for production use")
	
	return nil
}

// testRepoAccess checks if a repository URL is accessible and the branch exists
func testRepoAccess(url, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Use git ls-remote to check repository and branch accessibility
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", url, fmt.Sprintf("refs/heads/%s", branch))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot access repository: %w (output: %s)", err, string(output))
	}
	
	// Check if branch was found in the output
	if len(output) == 0 {
		return fmt.Errorf("branch '%s' not found in repository", branch)
	}
	
	return nil
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

func (w *RepoWatcher) updateWorkingDir(ctx context.Context) error {
	// Reset local branch to match remote branch
	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", fmt.Sprintf("origin/%s", w.config.Branch))
	cmd.Dir = w.repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git reset failed: %w", err)
	}
	return nil
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

	// Get current commit from remote
	commit, err := w.getCurrentCommit(ctx)
	if err != nil {
		return fmt.Errorf("getting commit: %w", err)
	}

	// Check if changed
	if commit != w.lastCommit {
		log.Printf("[%s] New commit detected: %s -> %s", w.config.Name, w.lastCommit[:8], commit[:8])

		// Update working directory to match remote
		if err := w.updateWorkingDir(ctx); err != nil {
			return fmt.Errorf("updating working directory: %w", err)
		}

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
