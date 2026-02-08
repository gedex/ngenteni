# Ngenteni - Git Repository Watcher

[![Tests](https://github.com/gedex/ngenteni/actions/workflows/test.yml/badge.svg)](https://github.com/gedex/ngenteni/actions/workflows/test.yml)

A simple Git repository watcher that monitors remote repositories for new commits and executes commands when changes are detected.

**Ngenteni** (Javanese: "waiting") - A lightweight tool that watches your Git repositories.

## Features

- Monitor multiple remote Git repositories simultaneously
- Configurable polling intervals per repository
- Execute custom commands when new commits are detected
- Pass repository information via environment variables
- Automatic repository cloning and fetching
- Dry-run mode to validate configuration before production use
- **Hot reload**: Automatically reload configuration when config file changes (no restart needed)

## Installation

### Download Pre-built Binaries

Download the latest release for your platform from the [releases page](https://github.com/gedex/ngenteni/releases).

#### Linux/macOS
```bash
# Download and extract (replace VERSION and OS/ARCH with your values)
wget https://github.com/gedex/ngenteni/releases/download/v0.1.0/ngenteni_0.1.0_Linux_x86_64.tar.gz
tar -xzf ngenteni_0.1.0_Linux_x86_64.tar.gz
sudo mv ngenteni /usr/local/bin/
```

#### Windows
Download the `.zip` file for Windows from the releases page and extract it.

### Using Go Install

```bash
go install github.com/gedex/ngenteni@latest
```

### Build from Source

```bash
git clone https://github.com/gedex/ngenteni.git
cd ngenteni
go build -o ngenteni main.go
```

## Configuration

Create a `config.json` file with your repositories:

```json
{
  "repos": [
    {
      "name": "my-project",
      "url": "https://github.com/username/my-project.git",
      "branch": "main",
      "interval": "30s",
      "command": "echo 'New commit detected!'",
      "workdir": "./repos"
    }
  ]
}
```

### Configuration Fields

- **name**: Unique identifier for the repository
- **url**: Git repository URL (HTTPS or SSH)
- **branch**: Branch to monitor
- **interval**: Polling interval (e.g., "30s", "1m", "5m")
- **command**: Shell command to execute when new commits are detected (supports pipes, redirects, quotes, etc.)
- **workdir**: Directory where repositories will be cloned
- **timeout** (optional): Maximum duration for command execution (e.g., "5m", "30s"). If not specified, commands run without timeout. Commands exceeding the timeout are killed with SIGKILL.

## Usage

```bash
# Check version
./ngenteni --version
./ngenteni -v

# Validate configuration (dry-run mode)
./ngenteni --dry-run config.json

# Use default config.json
./ngenteni

# Use custom config file
./ngenteni /path/to/config.json

# Stop gracefully
# Press Ctrl+C - watchers will stop cleanly after completing current operations
```

### Hot Reload

Ngenteni automatically watches the configuration file for changes and reloads without requiring a restart:

- **Add repositories**: New repositories in the config will start being monitored automatically
- **Remove repositories**: Removed repositories will have their watchers stopped gracefully
- **Update repositories**: Modified repository configurations (URL, branch, interval, command, etc.) will restart the watcher with new settings
- **No downtime**: Existing watchers continue running while the configuration is being reloaded
- **Validation**: New configuration is validated before being applied; invalid configurations are rejected with error logs

The file watcher includes debouncing (500ms) to handle rapid successive writes and properly handles file removal/recreation patterns used by many text editors.

Example log output when config changes:
```
2026/02/08 11:07:20 Config file changed, reloading...
2026/02/08 11:07:20 Config reloaded with 2 repos
2026/02/08 11:07:20 [repo2] New repository detected, starting watcher
2026/02/08 11:07:20 [repo1] Configuration unchanged, keeping watcher
2026/02/08 11:07:20 Config reloaded successfully
```

### Dry-Run Mode

Before running in production, you can validate your configuration using the `--dry-run` flag:

```bash
./ngenteni --dry-run config.json
```

This mode will:
- Load and validate your configuration file
- Test repository accessibility using `git ls-remote`
- Verify that branches exist
- Display what would happen without actually cloning repositories or executing commands
- Report any configuration errors or access issues

Example output:
```
2026/01/12 02:28:34 Loaded config with 1 repos
2026/01/12 02:28:34 Running in dry-run mode - no commands will be executed
2026/01/12 02:28:34 Validating configuration...
2026/01/12 02:28:34 ✓ Configuration is valid (1 repositories configured)

[1/1] Checking repository: ngenteni
2026/01/12 02:28:34   Testing repository accessibility: https://github.com/gedex/ngenteni.git
2026/01/12 02:28:34   ✓ Repository is accessible
2026/01/12 02:28:34   ✓ Polling interval: 30s
2026/01/12 02:28:34   ✓ Command timeout: 5m0s
2026/01/12 02:28:34   ✓ Work directory: /tmp/test-repos
2026/01/12 02:28:34   ✓ Command configured: echo 'New commit detected'
2026/01/12 02:28:34   ℹ Command would execute when new commits are detected

✓ All repositories validated successfully
2026/01/12 02:28:34 ✓ Configuration is ready for production use
2026/01/12 02:28:34 Dry-run completed successfully!
```

## Environment Variables

When a command is executed, the following environment variables are available:

- `REPO_NAME`: Repository name from config
- `REPO_PATH`: Local path to the cloned repository
- `REPO_URL`: Git repository URL
- `REPO_BRANCH`: Branch being monitored
- `OLD_COMMIT`: Previous commit SHA
- `NEW_COMMIT`: New commit SHA

### Command Syntax

Commands are executed through a shell (`sh -c`), which means you can use:
- Shell pipes: `command1 | command2`
- Redirects: `command > output.txt`
- Logical operators: `command1 && command2 || command3`
- Environment variables: `echo $REPO_NAME`
- Quotes and complex strings

**Inline commands:**
```json
{
  "command": "echo 'New commit in' $REPO_NAME '!' && /opt/notify.sh"
}
```

**Script files:**
```json
{
  "command": "/path/to/script.sh"
}
```

### Example Command Script

```bash
#!/bin/bash
# deploy.sh

echo "Repository: $REPO_NAME"
echo "New commit: $NEW_COMMIT"
echo "Path: $REPO_PATH"

# Example: Deploy application
cd "$REPO_PATH"
./deploy.sh
```

Make it executable and reference it in config:

```json
{
  "command": "/path/to/deploy.sh"
}
```

## Example Use Cases

### Continuous Deployment

```json
{
  "name": "prod-app",
  "url": "https://github.com/company/app.git",
  "branch": "main",
  "interval": "1m",
  "command": "/opt/scripts/deploy.sh",
  "workdir": "./repos"
}
```

### Notification on Changes

```json
{
  "name": "watched-repo",
  "url": "https://github.com/user/repo.git",
  "branch": "develop",
  "interval": "5m",
  "command": "curl -X POST https://hooks.slack.com/... -d '{\"text\": \"New commit in watched-repo\"}'",
  "workdir": "./repos"
}
```

### Run Tests

```json
{
  "name": "test-project",
  "url": "https://github.com/team/project.git",
  "branch": "main",
  "interval": "2m",
  "command": "make test",
  "workdir": "./repos"
}
```

## How It Works

1. **Initialization**: Clones repositories if they don't exist locally
2. **Monitoring**: Periodically runs `git fetch` to check for new commits on the remote branch
3. **Detection**: Compares remote branch commit SHA with the last known commit
4. **Synchronization**: Updates local working directory to match remote when new commits are detected
5. **Execution**: Runs the configured command with the latest files from the repository
6. **Repeat**: Continues monitoring at the specified interval

**Note**: When new commits are detected, ngenteni automatically syncs the local working directory (using `git reset --hard origin/<branch>`) before running your command. This ensures your command always works with the latest files.

## Requirements

- Go 1.16 or higher
- Git installed and available in PATH
- Network access to remote repositories

## Tips

- Use SSH URLs with SSH keys for private repositories
- Set appropriate intervals to avoid rate limiting
- Test your commands before deploying
- Use absolute paths for commands and scripts
- Check logs for any errors or issues

## Development

### Running Tests

```bash
# Quick test run
go test ./...

# Run a specific test function
go test -v -run TestLoadConfig

# Coverage summary
go test -cover ./...

# Visual HTML coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Building Locally

```bash
go build -o ngenteni main.go
```

### Creating a Release

This project uses [GoReleaser](https://goreleaser.com/) for releases. For detailed release instructions, see [docs/releasing.md](docs/releasing.md).

## License

MIT
