# TODO - Suggested Improvements

This document tracks potential improvements for ngenteni, prioritized by value and implementation complexity.

## High Priority (Essential)

### 1. Add `--version` flag
- **Why**: Users can't check what version they're running
- **Effort**: Low
- **Value**: High (essential for support/debugging)
- **Implementation**:
  ```go
  var version = "dev"
  if os.Args contains --version, print version and exit
  ```

### 2. Graceful shutdown handling
- **Why**: Watchers can't be stopped cleanly; Ctrl+C terminates mid-operation
- **Effort**: Low
- **Value**: High (prevents partial command execution)
- **Implementation**:
  ```go
  Handle SIGTERM/SIGINT, stop tickers, wait for in-flight commands
  ```

## Medium Priority (Quality of Life)

### 3. `--dry-run` mode
- **Why**: Test config without executing commands
- **Effort**: Low
- **Value**: Medium
- **Usage**:
  ```bash
  ngenteni --dry-run config.json
  ```

### 4. Structured logging with levels
- **Why**: Too verbose in production, not enough detail when debugging
- **Effort**: Medium
- **Value**: Medium
- **Implementation**:
  ```json
  {"log_level": "info"}  // debug, info, warn, error
  ```

### 5. Pass commit metadata to commands
- **Why**: Only SHA is available, not message/author/files
- **Effort**: Medium
- **Value**: Medium
- **Implementation**:
  ```bash
  COMMIT_MESSAGE, COMMIT_AUTHOR, COMMIT_FILES env vars
  ```

### 6. Stats/metrics tracking
- **Why**: No visibility into watcher activity
- **Effort**: Low
- **Value**: Medium
- **Example**:
  ```
  Log: "Checked 150 times, detected 3 changes, 2 command successes, 1 failure"
  ```

### 7. Retry logic for failed commands
- **Why**: Transient failures cause missed deployments
- **Effort**: Medium
- **Value**: Medium
- **Implementation**:
  ```json
  {"retry": {"attempts": 3, "delay": "30s"}}
  ```

## Low Priority (Nice to Have)

### 8. Config file watching
- **Why**: Must restart to update config
- **Effort**: Medium
- **Value**: Low (restart is acceptable)

### 9. Multiple branches per repo
- **Why**: Limited to one branch per repo
- **Effort**: Medium
- **Value**: Low (can add multiple repos with same URL)
- **Note**: Current workaround is to add multiple repo entries with the same URL

### 10. Initial run option
- **Why**: Command only runs on new commits, not on startup
- **Effort**: Low
- **Value**: Low
- **Implementation**:
  ```json
  {"run_on_start": true}
  ```

### 11. Webhook mode
- **Why**: Polling wastes resources
- **Effort**: High
- **Value**: Low (polling is fine for most cases)
- **Note**: Would require running an HTTP server

### 12. Better auth for private repos
- **Why**: SSH keys work but could add token support
- **Effort**: Medium
- **Value**: Low (SSH works well)
- **Implementation**: Support `GIT_ASKPASS` or similar token-based auth

## Top 3 Recommendations

~~If implementing only 3 features, prioritize:~~

~~1. **Graceful shutdown** - Essential for production use~~
~~2. **`--version` flag** - Takes 5 minutes, very useful~~
~~3. **Command timeout** - Prevents hung watchers~~

✅ **All high-priority recommendations completed!**

New recommendations for next priorities:

1. **`--dry-run` mode** (Item 3) - Test configs without executing commands
2. **Structured logging** (Item 4) - Better debugging and production logs
3. **Pass commit metadata** (Item 5) - More context for commands

## Contributing

When implementing features from this list:

1. Update this document to mark items as in-progress or completed
2. Add tests for new functionality
3. Update README.md with new features/usage
4. Update config.example.json if config format changes
5. Follow existing code style and patterns

## Completed Items

### ✅ 1. Add `--version` flag (Completed 2026-01-11)
- Added version variables with build-time injection via ldflags
- Supports both `--version` and `-v` flags
- Displays version, commit, build date, and builder info
- Updated GoReleaser configuration for version injection

### ✅ 2. Graceful shutdown handling (Completed 2026-01-11)
- Implemented signal handling for SIGINT and SIGTERM
- Context-based cancellation propagation through all goroutines
- Watchers stop cleanly after completing current operations
- Clear log messages during shutdown process
- Exit code 0 on clean shutdown

### ✅ 5. Command timeout configuration (Completed 2026-01-11)
- Added optional `timeout` field to repo configuration
- Commands exceeding timeout are killed with clear error logging
- Backward compatible - existing configs without timeout work unchanged
- Timeout validation at startup with helpful error messages
- Updated config.example.json and README.md with timeout documentation

### ✅ 3. Add tests (Completed 2026-01-11)
- Created comprehensive test suite in main_test.go
- Config loading tests: Valid/invalid JSON, missing files, multiple repos, optional timeout
- Duration validation tests: Valid/invalid intervals and timeouts, negative values
- Command execution tests: Success/failure, timeout behavior, empty commands, shell features (pipes, logical operators)
- Environment variable tests: Verifies all env vars are passed correctly
- Context cancellation tests: Tests graceful shutdown handling
- Test coverage: 47.3% of statements
- Function-level coverage:
  - loadConfig: 100%
  - clone: 100%
  - runCommand: 100%
  - NewRepoWatcher: 88.2%
  - getCurrentCommit: 83.3%
  - setup: 75%
- Uses table-driven tests with t.Run() for organization
- Automatic cleanup with t.TempDir()
- Test helper functions with t.Helper()
- Local git repositories for integration tests

### ✅ 4. Config validation (Completed 2026-01-12)
- Validates all configurations at startup before creating watchers (fail-fast)
- Validates required fields: name, url, branch, interval, command, workdir
- Validates interval format and ensures positive duration
- Validates timeout format (if provided) and ensures positive duration
- Clear error messages indicating which repo and which field has issues
- Comprehensive test coverage for all validation scenarios
- Test coverage improved from 47.8% to 58.7%
- All validation functions: 100% coverage
