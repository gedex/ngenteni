package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type RepoConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Branch   string `json:"branch"`
	Interval string `json:"interval"`
	Command  string `json:"command"`
	WorkDir  string `json:"workdir"`
}

type Config struct {
	Repos []RepoConfig `json:"repos"`
}

type RepoWatcher struct {
	config     RepoConfig
	repoPath   string
	lastCommit string
	interval   time.Duration
}

func main() {
	// Read config file
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Loaded config with %d repos", len(config.Repos))

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
			watcher.Watch()
		}(repo)
	}

	log.Println("All watchers started. Press Ctrl+C to stop.")
	wg.Wait()
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

	// Create workdir if not exists
	if err := os.MkdirAll(config.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("creating workdir: %w", err)
	}

	repoPath := filepath.Join(config.WorkDir, config.Name)

	watcher := &RepoWatcher{
		config:   config,
		repoPath: repoPath,
		interval: interval,
	}

	// Initial setup
	if err := watcher.setup(); err != nil {
		return nil, err
	}

	return watcher, nil
}

func (w *RepoWatcher) setup() error {
	// Check if repo exists
	if _, err := os.Stat(filepath.Join(w.repoPath, ".git")); os.IsNotExist(err) {
		log.Printf("[%s] Cloning repository...", w.config.Name)
		if err := w.clone(); err != nil {
			return fmt.Errorf("cloning repo: %w", err)
		}
	} else {
		log.Printf("[%s] Repository already exists", w.config.Name)
	}

	// Get initial commit
	commit, err := w.getCurrentCommit()
	if err != nil {
		return fmt.Errorf("getting initial commit: %w", err)
	}
	w.lastCommit = commit
	log.Printf("[%s] Initial commit: %s", w.config.Name, commit[:8])

	return nil
}

func (w *RepoWatcher) clone() error {
	cmd := exec.Command("git", "clone", "--branch", w.config.Branch, w.config.URL, w.repoPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (w *RepoWatcher) fetch() error {
	cmd := exec.Command("git", "fetch", "origin", w.config.Branch)
	cmd.Dir = w.repoPath
	return cmd.Run()
}

func (w *RepoWatcher) getCurrentCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", fmt.Sprintf("origin/%s", w.config.Branch))
	cmd.Dir = w.repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (w *RepoWatcher) Watch() {
	log.Printf("[%s] Watching every %s", w.config.Name, w.interval)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := w.check(); err != nil {
			log.Printf("[%s] Error checking: %v", w.config.Name, err)
		}
	}
}

func (w *RepoWatcher) check() error {
	// Fetch latest changes
	if err := w.fetch(); err != nil {
		return fmt.Errorf("fetching: %w", err)
	}

	// Get current commit
	commit, err := w.getCurrentCommit()
	if err != nil {
		return fmt.Errorf("getting commit: %w", err)
	}

	// Check if changed
	if commit != w.lastCommit {
		log.Printf("[%s] New commit detected: %s -> %s", w.config.Name, w.lastCommit[:8], commit[:8])

		// Run command
		if err := w.runCommand(commit); err != nil {
			log.Printf("[%s] Command failed: %v", w.config.Name, err)
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

	// Execute command through shell to properly handle quotes, pipes, etc.
	cmd := exec.Command("sh", "-c", w.config.Command)
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

	return cmd.Run()
}
