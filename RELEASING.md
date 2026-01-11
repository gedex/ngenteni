# Release Guide

This document describes how to create a new release for ngenteni.

## Prerequisites

- Push access to the repository
- [GoReleaser](https://goreleaser.com/) installed (for local releases)
- GitHub token with `repo` permissions (for local releases)

## Automated Release (Recommended)

Releases are automatically created via GitHub Actions when you push a new tag.

### Steps

1. Ensure all changes are committed and pushed to `main` branch
2. Update version references in README if needed
3. Create and push a new tag:

```bash
# Create an annotated tag
git tag -a v0.1.0 -m "Release v0.1.0: Initial release"

# Push the tag
git push origin v0.1.0
```

4. GitHub Actions will automatically:
   - Build binaries for all platforms
   - Create a GitHub release
   - Upload release artifacts
   - Generate checksums

5. Monitor the release at: https://github.com/gedex/ngenteni/actions

## Manual Release (Local)

For testing or when you need to release manually:

### Test Release (Snapshot)

Test the release process without publishing:

```bash
goreleaser --snapshot --skip-publish --rm-dist
```

This creates release artifacts in the `dist/` directory for inspection.

### Publish Release

```bash
# Set your GitHub token
export GITHUB_TOKEN="your_github_token_here"

# Create a tag locally
git tag -a v0.1.0 -m "Release v0.1.0"

# Run goreleaser
goreleaser --rm-dist

# Push the tag
git push origin v0.1.0
```

## Versioning

Follow [Semantic Versioning](https://semver.org/):

- **Major** (v1.0.0): Breaking changes
- **Minor** (v0.1.0): New features, backward compatible
- **Patch** (v0.0.1): Bug fixes, backward compatible

## Release Checklist

Before creating a release:

- [ ] All tests pass
- [ ] Documentation is up to date
- [ ] CHANGELOG is updated (if exists)
- [ ] Version references in README are updated
- [ ] config.example.json is up to date
- [ ] Test build locally: `go build -o ngenteni main.go`
- [ ] Test goreleaser: `goreleaser --snapshot --skip-publish --rm-dist`

## Release Assets

Each release includes:

- **Binaries**: For Linux, macOS, and Windows (multiple architectures)
- **Archives**: `.tar.gz` for Unix, `.zip` for Windows
- **Checksums**: SHA256 checksums for all artifacts
- **Source Code**: Automatic GitHub source archives

## Troubleshooting

### GitHub Actions Fails

Check the workflow run at https://github.com/gedex/ngenteni/actions

Common issues:
- Missing GITHUB_TOKEN: Should be automatically provided by GitHub Actions
- Build failures: Check Go version and dependencies
- Tag already exists: Delete and recreate the tag

### Local Release Fails

Common issues:
- Missing GITHUB_TOKEN: Set the environment variable
- Git state not clean: Commit or stash changes
- Tag already exists: Delete the tag first: `git tag -d v0.1.0`

## Post-Release

After a successful release:

1. Announce the release (if applicable)
2. Update any deployment documentation
3. Monitor for issues and user feedback
4. Plan next release based on feedback
