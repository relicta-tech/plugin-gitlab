# Relicta GitLab Plugin

Official GitLab plugin for [Relicta](https://github.com/relicta-tech/relicta) - AI-powered release management.

## Features

- Create GitLab releases automatically
- Upload release assets to GitLab's generic package registry
- Support for external asset links
- Associate milestones with releases
- Self-hosted GitLab instance support

## Installation

```bash
relicta plugin install gitlab
relicta plugin enable gitlab
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: gitlab
    enabled: true
    config:
      project_id: "group/project"  # or numeric project ID
      # base_url: "https://gitlab.example.com"  # for self-hosted
      assets:
        - "dist/*.tar.gz"
        - "dist/*.zip"
      asset_links:
        - name: "Documentation"
          url: "https://docs.example.com"
          link_type: "runbook"
```

### Environment Variables

- `GITLAB_TOKEN` - GitLab personal access token (required)
- `GL_TOKEN` - Alternative token variable name

### Configuration Options

| Option | Description | Required |
|--------|-------------|----------|
| `project_id` | GitLab project ID or path (e.g., "group/project") | Yes |
| `base_url` | GitLab instance URL (default: https://gitlab.com) | No |
| `token` | GitLab token (prefer using env var) | No |
| `name` | Release name (default: "Release {version}") | No |
| `description` | Release description (uses release notes if empty) | No |
| `ref` | Tag ref for the release | No |
| `released_at` | Release date in ISO 8601 format | No |
| `milestones` | List of milestones to associate | No |
| `assets` | List of files to upload | No |
| `asset_links` | External asset links | No |

### Asset Links

Asset links can have the following properties:

```yaml
asset_links:
  - name: "Package Documentation"
    url: "https://docs.example.com/v1.0.0"
    filepath: "/docs"
    link_type: "runbook"  # other, runbook, image, package
```

## Token Permissions

The GitLab token requires the following scopes:
- `api` - Full API access for creating releases and uploading packages

## Hooks

This plugin responds to the following hooks:

- `post_publish` - Creates the GitLab release
- `on_success` - Acknowledges successful release
- `on_error` - Acknowledges failed release

## Development

```bash
# Build
go build -o gitlab .

# Test locally
relicta plugin install ./gitlab
relicta plugin enable gitlab
relicta publish --dry-run
```

## License

MIT License - see [LICENSE](LICENSE) for details.
