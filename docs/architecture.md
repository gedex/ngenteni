# Ngenteni Architecture

This document describes the internal architecture and workflow of ngenteni, a Git repository watcher that monitors remote repositories for new commits and executes commands when changes are detected.

## Table of Contents

- [Overview](#overview)
- [Core Components](#core-components)
- [System Architecture](#system-architecture)
- [Workflow Diagrams](#workflow-diagrams)
- [Data Structures](#data-structures)
- [Key Processes](#key-processes)
- [Error Handling](#error-handling)

## Overview

Ngenteni is a lightweight, single-binary tool written in Go that:
1. Monitors multiple Git repositories simultaneously
2. Detects new commits by polling remote branches
3. Synchronizes local working directories
4. Executes user-defined commands when changes occur
5. Supports graceful shutdown and timeout management

**Design Principles:**
- Simple: No dependencies, single binary
- Reliable: Fail-fast validation, graceful shutdown
- Observable: Clear logging, contextual error messages
- Configurable: JSON-based configuration

## Core Components

```mermaid
graph TB
    subgraph "Main Process"
        Main[main.go]
        Config[Config Loader]
        Validator[Config Validator]
        SignalHandler[Signal Handler]
    end

    subgraph "Watcher Management"
        WM[Watcher Manager]
        W1[RepoWatcher 1]
        W2[RepoWatcher 2]
        WN[RepoWatcher N]
    end

    subgraph "Repository Operations"
        Git[Git Operations]
        Clone[Clone]
        Fetch[Fetch]
        GetCommit[Get Current Commit]
        Update[Update Working Dir]
    end

    subgraph "Command Execution"
        CmdExec[Command Executor]
        Shell[Shell /bin/sh]
        Env[Environment Variables]
    end

    Main --> Config
    Config --> Validator
    Validator --> WM
    Main --> SignalHandler
    SignalHandler --> WM

    WM --> W1
    WM --> W2
    WM --> WN

    W1 --> Git
    W2 --> Git
    WN --> Git

    Git --> Clone
    Git --> Fetch
    Git --> GetCommit
    Git --> Update

    W1 --> CmdExec
    W2 --> CmdExec
    WN --> CmdExec

    CmdExec --> Shell
    CmdExec --> Env

    style Main fill:#e1f5ff
    style WM fill:#fff3e0
    style Git fill:#f3e5f5
    style CmdExec fill:#e8f5e9
```

### Component Responsibilities

| Component | Responsibility |
|-----------|---------------|
| **Config Loader** | Reads and parses JSON configuration file |
| **Config Validator** | Validates all configuration fields at startup (fail-fast) |
| **Signal Handler** | Handles SIGINT/SIGTERM for graceful shutdown |
| **Watcher Manager** | Creates and manages multiple RepoWatcher goroutines |
| **RepoWatcher** | Monitors a single repository, detects changes, runs commands |
| **Git Operations** | Executes git commands (clone, fetch, rev-parse, reset) |
| **Command Executor** | Runs user commands with timeout and environment variables |

## System Architecture

### Concurrency Model

```mermaid
graph LR
    subgraph "Main Goroutine"
        M[Main Thread]
        SH[Signal Handler]
    end

    subgraph "Watcher Goroutines"
        W1[Watcher 1<br/>Goroutine]
        W2[Watcher 2<br/>Goroutine]
        WN[Watcher N<br/>Goroutine]
    end

    subgraph "Synchronization"
        WG[WaitGroup]
        CTX[Context<br/>Cancellation]
    end

    M -->|spawn| W1
    M -->|spawn| W2
    M -->|spawn| WN

    M --> WG
    W1 -.->|Done| WG
    W2 -.->|Done| WG
    WN -.->|Done| WG

    SH -->|Cancel| CTX
    CTX -.->|cancelled| W1
    CTX -.->|cancelled| W2
    CTX -.->|cancelled| WN

    WG -->|Wait| M

    style M fill:#e1f5ff
    style SH fill:#ffebee
    style WG fill:#f3e5f5
    style CTX fill:#fff3e0
```

**Key Characteristics:**
- Each repository runs in its own goroutine
- Context-based cancellation for graceful shutdown
- WaitGroup ensures all watchers complete before exit
- No shared state between watchers (isolation)

## Workflow Diagrams

### 1. Initialization Flow

```mermaid
sequenceDiagram
    participant User
    participant Main
    participant Config
    participant Validator
    participant Watcher
    participant Git

    User->>Main: Start ngenteni config.json
    Main->>Main: Parse flags (--version, -v)
    Main->>Config: loadConfig(path)
    Config->>Config: Read JSON file
    Config->>Config: Parse JSON
    Config->>Validator: validateConfig()

    alt Invalid Config
        Validator-->>Main: Error (fail fast)
        Main-->>User: Exit with error message
    else Valid Config
        Validator-->>Config: OK
        Config-->>Main: Config object

        Main->>Main: Setup signal handler
        Main->>Main: Create context with cancel

        loop For each repo
            Main->>Watcher: NewRepoWatcher(config)
            Watcher->>Watcher: Parse interval & timeout
            Watcher->>Git: setup() - clone if needed

            alt Repo doesn't exist
                Git->>Git: Clone repository
            else Repo exists
                Git->>Git: Open existing repo
            end

            Git->>Git: getCurrentCommit()
            Git-->>Watcher: Initial commit SHA
            Watcher-->>Main: RepoWatcher instance

            Main->>Watcher: Go Watch(ctx)
        end

        Main->>Main: Wait for signal or error
    end
```

### 2. Watch Loop Flow

```mermaid
stateDiagram-v2
    [*] --> Waiting

    Waiting --> CheckContext: Timer tick
    CheckContext --> Fetching: Context OK
    CheckContext --> Shutdown: Context cancelled

    Fetching --> GetCommit: git fetch success
    Fetching --> LogError: git fetch failed

    GetCommit --> CompareCommit: Got commit SHA
    GetCommit --> LogError: Failed to get commit

    CompareCommit --> NoChange: Same as last commit
    CompareCommit --> NewCommit: Different commit

    NoChange --> Waiting

    NewCommit --> UpdateWorkDir: Detected change
    UpdateWorkDir --> RunCommand: git reset --hard success
    UpdateWorkDir --> LogError: git reset failed

    RunCommand --> Success: Command succeeded
    RunCommand --> Timeout: Command timed out
    RunCommand --> Failed: Command failed

    Success --> UpdateLastCommit
    Timeout --> LogError
    Failed --> LogError

    UpdateLastCommit --> Waiting
    LogError --> Waiting

    Shutdown --> [*]
```

### 3. Change Detection and Execution

```mermaid
flowchart TD
    Start([Timer Tick]) --> CheckCtx{Context<br/>Cancelled?}
    CheckCtx -->|Yes| Return([Return ctx.Err])
    CheckCtx -->|No| Fetch[git fetch origin branch]

    Fetch --> GetRemote[git rev-parse origin/branch]
    GetRemote --> Compare{New Commit<br/>SHA?}

    Compare -->|Same| Wait([Wait for next tick])
    Compare -->|Different| Log1[Log: New commit detected]

    Log1 --> Reset[git reset --hard origin/branch]
    Reset --> SetupCmd[Setup Command Context]

    SetupCmd --> HasTimeout{Timeout<br/>Configured?}
    HasTimeout -->|Yes| WithTimeout[context.WithTimeout]
    HasTimeout -->|No| Background[context.Background]

    WithTimeout --> BuildCmd[Build sh -c command]
    Background --> BuildCmd

    BuildCmd --> SetEnv[Set Environment Variables:<br/>REPO_NAME, REPO_PATH,<br/>REPO_URL, REPO_BRANCH,<br/>OLD_COMMIT, NEW_COMMIT]

    SetEnv --> ExecCmd[exec.CommandContext]
    ExecCmd --> CmdResult{Command<br/>Result?}

    CmdResult -->|Success| LogSuccess[Log: Command executed successfully]
    CmdResult -->|Timeout| LogTimeout[Log: Command timeout]
    CmdResult -->|Error| LogFailed[Log: Command failed]

    LogSuccess --> UpdateCommit[Update lastCommit = newCommit]
    LogTimeout --> UpdateCommit
    LogFailed --> UpdateCommit

    UpdateCommit --> Wait

    style Start fill:#e1f5ff
    style CheckCtx fill:#fff3e0
    style Compare fill:#fff3e0
    style CmdResult fill:#fff3e0
    style Log1 fill:#c8e6c9
    style LogSuccess fill:#c8e6c9
    style LogTimeout fill:#ffccbc
    style LogFailed fill:#ffccbc
```

### 4. Graceful Shutdown Flow

```mermaid
sequenceDiagram
    participant User
    participant SignalHandler
    participant Context
    participant Watcher1
    participant Watcher2
    participant WaitGroup
    participant Main

    User->>SignalHandler: Ctrl+C (SIGINT)
    SignalHandler->>SignalHandler: Receive signal
    SignalHandler->>Main: Log: Received shutdown signal
    SignalHandler->>Context: cancel()

    Context-->>Watcher1: ctx.Done() closed
    Context-->>Watcher2: ctx.Done() closed

    par Watcher Shutdown
        Watcher1->>Watcher1: Check context in select
        Watcher1->>Watcher1: Return from Watch()
        Watcher1->>WaitGroup: wg.Done()
    and
        Watcher2->>Watcher2: Check context in select
        Watcher2->>Watcher2: Return from Watch()
        Watcher2->>WaitGroup: wg.Done()
    end

    Main->>WaitGroup: wg.Wait()
    WaitGroup-->>Main: All goroutines finished
    Main->>Main: Log: All watchers stopped gracefully
    Main->>User: Exit 0
```

## Data Structures

### Configuration Structure

```go
type Config struct {
    Repos []RepoConfig `json:"repos"`
}

type RepoConfig struct {
    Name     string `json:"name"`          // Required: Unique identifier
    URL      string `json:"url"`           // Required: Git repository URL
    Branch   string `json:"branch"`        // Required: Branch to monitor
    Interval string `json:"interval"`      // Required: Polling interval (e.g., "30s")
    Command  string `json:"command"`       // Required: Command to execute
    WorkDir  string `json:"workdir"`       // Required: Local clone directory
    Timeout  string `json:"timeout,omitempty"` // Optional: Command timeout
}
```

### Runtime Structure

```go
type RepoWatcher struct {
    config         RepoConfig      // Configuration for this repo
    repoPath       string          // Full path to local clone
    lastCommit     string          // Last known commit SHA
    interval       time.Duration   // Parsed polling interval
    commandTimeout time.Duration   // Parsed command timeout (0 = no timeout)
}
```

## Key Processes

### Config Validation Process

```mermaid
flowchart TD
    Start([loadConfig called]) --> Read[Read JSON file]
    Read --> Parse[Unmarshal JSON]
    Parse --> Validate[validateConfig]

    Validate --> CheckEmpty{repos<br/>empty?}
    CheckEmpty -->|Yes| ErrEmpty[Error: no repositories configured]
    CheckEmpty -->|No| LoopRepos[For each repo]

    LoopRepos --> ValidateRepo[validateRepoConfig]

    ValidateRepo --> CheckName{name<br/>empty?}
    CheckName -->|Yes| ErrName[Error: name is required]
    CheckName -->|No| CheckURL{url<br/>empty?}

    CheckURL -->|Yes| ErrURL[Error: url is required]
    CheckURL -->|No| CheckBranch{branch<br/>empty?}

    CheckBranch -->|Yes| ErrBranch[Error: branch is required]
    CheckBranch -->|No| CheckInterval{interval<br/>empty?}

    CheckInterval -->|Yes| ErrInterval[Error: interval is required]
    CheckInterval -->|No| ParseInterval[time.ParseDuration]

    ParseInterval -->|Invalid| ErrIntervalFormat[Error: invalid interval]
    ParseInterval -->|Valid| CheckPositive{interval > 0?}

    CheckPositive -->|No| ErrIntervalPos[Error: must be positive]
    CheckPositive -->|Yes| CheckTimeout{timeout<br/>provided?}

    CheckTimeout -->|Yes| ParseTimeout[time.ParseDuration]
    CheckTimeout -->|No| NextRepo{More<br/>repos?}

    ParseTimeout -->|Invalid| ErrTimeoutFormat[Error: invalid timeout]
    ParseTimeout -->|Valid| CheckTimeoutPos{timeout > 0?}

    CheckTimeoutPos -->|No| ErrTimeoutPos[Error: must be positive]
    CheckTimeoutPos -->|Yes| NextRepo

    NextRepo -->|Yes| LoopRepos
    NextRepo -->|No| Success([Return Config])

    ErrEmpty --> FailFast([Exit with error])
    ErrName --> FailFast
    ErrURL --> FailFast
    ErrBranch --> FailFast
    ErrInterval --> FailFast
    ErrIntervalFormat --> FailFast
    ErrIntervalPos --> FailFast
    ErrTimeoutFormat --> FailFast
    ErrTimeoutPos --> FailFast

    style Start fill:#e1f5ff
    style Success fill:#c8e6c9
    style FailFast fill:#ffccbc
```

### Git Operations

**Clone Operation:**
```bash
git clone --branch <branch> <url> <path>
```

**Fetch Operation:**
```bash
git fetch origin <branch>
```

**Get Current Commit:**
```bash
git rev-parse origin/<branch>
```

**Update Working Directory:**
```bash
git reset --hard origin/<branch>
```

**Why `git reset --hard`?**
- Ensures working directory matches remote exactly
- Discards any local changes (expected behavior)
- Faster than pulling and merging
- Simpler for read-only monitoring use case

### Command Execution

**Environment Variables Passed to Commands:**

| Variable | Description | Example |
|----------|-------------|---------|
| `REPO_NAME` | Repository name from config | `my-project` |
| `REPO_PATH` | Local clone path | `/path/to/repos/my-project` |
| `REPO_URL` | Git repository URL | `https://github.com/user/repo.git` |
| `REPO_BRANCH` | Monitored branch | `main` |
| `OLD_COMMIT` | Previous commit SHA | `abc123def456...` |
| `NEW_COMMIT` | New commit SHA | `def456abc789...` |

## Error Handling

### Error Handling Strategy

| Error Type | Behavior | Recovery |
|------------|----------|----------|
| **Config Error** | Fail fast at startup | Fix config and restart |
| **Git Clone Error** | Log error, skip repo | Fix URL/access, restart |
| **Git Fetch Error** | Log error, retry next interval | Transient - auto-recovers |
| **Command Error** | Log error, continue watching | Check command, auto-retries on next commit |
| **Command Timeout** | Kill command, log error, continue | Adjust timeout or fix command |
| **Context Cancelled** | Stop gracefully, no error | Intentional shutdown |

### Logging Strategy

**Log Format:**
```
2026/01/12 10:30:45 [repo-name] Message
```
