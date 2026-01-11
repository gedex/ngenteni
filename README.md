# Ngenteni - Git Repository Watcher

A simple Git repository watcher that monitors remote repositories for new commits and executes commands when changes are detected.

**Ngenteni** (Indonesian: "waiting") - A lightweight tool that watches your Git repositories.

## Features

- Monitor multiple remote Git repositories simultaneously
- Configurable polling intervals per repository
- Execute custom commands when new commits are detected
- Pass repository information via environment variables
- Automatic repository cloning and fetching

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

## Usage

```bash
# Use default config.json
./ngenteni

# Use custom config file
./ngenteni /path/to/config.json
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
2. **Monitoring**: Periodically runs `git fetch` to check for new commits
3. **Detection**: Compares current commit SHA with the last known commit
4. **Execution**: Runs the configured command when new commits are detected
5. **Repeat**: Continues monitoring at the specified interval

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
go test ./...
```

### Building Locally

```bash
go build -o ngenteni main.go
```

### Creating a Release

This project uses [GoReleaser](https://goreleaser.com/) for releases. For detailed release instructions, see [RELEASING.md](RELEASING.md).

## License

MIT
