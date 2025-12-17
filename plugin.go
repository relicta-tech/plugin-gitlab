// Package main implements the GitLab plugin for Relicta.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// GitLabPlugin implements the GitLab release plugin.
type GitLabPlugin struct{}

// Config represents the GitLab plugin configuration.
type Config struct {
	// BaseURL is the GitLab instance URL (default: https://gitlab.com).
	BaseURL string `json:"base_url,omitempty"`
	// ProjectID is the GitLab project ID or path (e.g., "group/project").
	ProjectID string `json:"project_id,omitempty"`
	// Token is the GitLab personal access token.
	Token string `json:"token,omitempty"`
	// Name is the release name (default: "Release {version}").
	Name string `json:"name,omitempty"`
	// Description is the release description (uses release notes if empty).
	Description string `json:"description,omitempty"`
	// Ref is the tag ref for the release.
	Ref string `json:"ref,omitempty"`
	// ReleasedAt is the release date (optional).
	ReleasedAt string `json:"released_at,omitempty"`
	// Milestones is a list of milestones to associate with the release.
	Milestones []string `json:"milestones,omitempty"`
	// Assets is a list of files to upload as release assets.
	Assets []string `json:"assets,omitempty"`
	// AssetLinks is a list of external asset links.
	AssetLinks []AssetLink `json:"asset_links,omitempty"`
}

// AssetLink represents an external asset link for the release.
type AssetLink struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	FilePath string `json:"filepath,omitempty"`
	LinkType string `json:"link_type,omitempty"` // "other", "runbook", "image", "package"
}

// GetInfo returns plugin metadata.
func (p *GitLabPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "gitlab",
		Version:     "2.0.0",
		Description: "Create GitLab releases and upload assets",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
			plugin.HookOnError,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {
				"base_url": {"type": "string", "description": "GitLab instance URL (default: https://gitlab.com)"},
				"project_id": {"type": "string", "description": "Project ID or path (e.g., 'group/project')"},
				"token": {"type": "string", "description": "GitLab token (or use GITLAB_TOKEN env)"},
				"name": {"type": "string", "description": "Release name (default: 'Release {version}')"},
				"description": {"type": "string", "description": "Release description"},
				"ref": {"type": "string", "description": "Tag ref for the release"},
				"released_at": {"type": "string", "description": "Release date (ISO 8601)"},
				"milestones": {"type": "array", "items": {"type": "string"}, "description": "Associated milestones"},
				"assets": {"type": "array", "items": {"type": "string"}, "description": "Files to upload"},
				"asset_links": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"url": {"type": "string"},
							"filepath": {"type": "string"},
							"link_type": {"type": "string", "enum": ["other", "runbook", "image", "package"]}
						},
						"required": ["name", "url"]
					},
					"description": "External asset links"
				}
			}
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *GitLabPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish:
		return p.createRelease(ctx, cfg, req.Context, req.DryRun)
	case plugin.HookOnSuccess:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Release successful",
		}, nil
	case plugin.HookOnError:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Release failed notification acknowledged",
		}, nil
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// createRelease creates a GitLab release.
func (p *GitLabPlugin) createRelease(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Get GitLab client
	client, err := p.getClient(cfg)
	if err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create GitLab client: %v", err),
		}, nil
	}

	// Get project ID
	projectID := cfg.ProjectID
	if projectID == "" {
		// Try to construct from repository info
		if releaseCtx.RepositoryOwner != "" && releaseCtx.RepositoryName != "" {
			projectID = fmt.Sprintf("%s/%s", releaseCtx.RepositoryOwner, releaseCtx.RepositoryName)
		}
	}

	if projectID == "" {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   "project_id is required (set in config or provide repository owner/name)",
		}, nil
	}

	// Prepare release
	tagName := releaseCtx.TagName
	name := cfg.Name
	if name == "" {
		name = fmt.Sprintf("Release %s", releaseCtx.Version)
	}

	description := cfg.Description
	if description == "" {
		description = releaseCtx.ReleaseNotes
		if description == "" {
			description = releaseCtx.Changelog
		}
	}

	ref := cfg.Ref
	if ref == "" {
		ref = tagName
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Would create GitLab release for %s: %s", projectID, tagName),
			Outputs: map[string]any{
				"tag_name":   tagName,
				"project_id": projectID,
				"name":       name,
			},
		}, nil
	}

	// Build release options
	releaseOpts := &gitlab.CreateReleaseOptions{
		Name:        &name,
		TagName:     &tagName,
		Description: &description,
		Ref:         &ref,
	}

	// Add milestones if specified
	if len(cfg.Milestones) > 0 {
		milestones := make([]string, len(cfg.Milestones))
		copy(milestones, cfg.Milestones)
		releaseOpts.Milestones = &milestones
	}

	// Add asset links
	if len(cfg.AssetLinks) > 0 {
		links := make([]*gitlab.ReleaseAssetLinkOptions, len(cfg.AssetLinks))
		for i, link := range cfg.AssetLinks {
			var linkType *gitlab.LinkTypeValue
			if link.LinkType != "" {
				lt := gitlab.LinkTypeValue(link.LinkType)
				linkType = &lt
			} else {
				linkType = gitlab.Ptr(gitlab.OtherLinkType)
			}

			releaseLink := &gitlab.ReleaseAssetLinkOptions{
				Name:     gitlab.Ptr(link.Name),
				URL:      gitlab.Ptr(link.URL),
				LinkType: linkType,
			}
			if link.FilePath != "" {
				releaseLink.DirectAssetPath = gitlab.Ptr(link.FilePath)
			}
			links[i] = releaseLink
		}
		releaseOpts.Assets = &gitlab.ReleaseAssetsOptions{
			Links: links,
		}
	}

	// Create release
	release, _, err := client.Releases.CreateRelease(projectID, releaseOpts, gitlab.WithContext(ctx))
	if err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create release: %v", err),
		}, nil
	}

	// Upload file assets
	var artifacts []plugin.Artifact
	for _, assetPath := range cfg.Assets {
		artifact, err := p.uploadAsset(ctx, client, projectID, tagName, assetPath)
		if err != nil {
			// Log but don't fail
			continue
		}
		artifacts = append(artifacts, *artifact)
	}

	// Construct release URL
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	releaseURL := fmt.Sprintf("%s/%s/-/releases/%s", strings.TrimSuffix(baseURL, "/"), projectID, tagName)

	return &plugin.ExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("Created GitLab release: %s", releaseURL),
		Outputs: map[string]any{
			"release_url": releaseURL,
			"tag_name":    release.TagName,
			"name":        release.Name,
		},
		Artifacts: artifacts,
	}, nil
}

// validateAssetPath validates and sanitizes an asset path to prevent path traversal.
// It ensures the path stays within the current working directory.
func validateAssetPath(assetPath string) (string, error) {
	if assetPath == "" {
		return "", fmt.Errorf("asset path cannot be empty")
	}

	// Get current working directory as the security boundary
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Clean the path to normalize it
	cleanPath := filepath.Clean(assetPath)

	// Prevent obvious path traversal attempts
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "/../") {
		return "", fmt.Errorf("path traversal not allowed in asset paths: %s", assetPath)
	}

	// Resolve to absolute path
	var absPath string
	if filepath.IsAbs(cleanPath) {
		absPath = cleanPath
	} else {
		absPath = filepath.Join(cwd, cleanPath)
	}

	// Resolve symlinks to get the real path and prevent symlink attacks
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If EvalSymlinks fails, the path might not exist
		return "", fmt.Errorf("asset file not accessible: %w", err)
	}

	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolvedCwd = cwd
	}

	// Verify the resolved path is within the working directory
	// Add trailing separator to prevent partial path matches (e.g., /home/user2 vs /home/user)
	if !strings.HasPrefix(resolvedPath+string(filepath.Separator), resolvedCwd+string(filepath.Separator)) &&
		resolvedPath != resolvedCwd {
		return "", fmt.Errorf("asset path must be within the current working directory")
	}

	return resolvedPath, nil
}

// uploadAsset uploads a release asset to GitLab's generic package registry.
func (p *GitLabPlugin) uploadAsset(ctx context.Context, client *gitlab.Client, projectID, tagName, assetPath string) (*plugin.Artifact, error) {
	// Validate and sanitize the asset path to prevent path traversal
	validatedPath, err := validateAssetPath(assetPath)
	if err != nil {
		return nil, fmt.Errorf("invalid asset path %s: %w", assetPath, err)
	}

	// Verify file exists and is a regular file (not a directory)
	info, err := os.Lstat(validatedPath)
	if err != nil {
		return nil, fmt.Errorf("asset file not accessible %s: %w", assetPath, err)
	}

	// Reject directories
	if info.IsDir() {
		return nil, fmt.Errorf("asset path is a directory, not a file: %s", assetPath)
	}

	// Note: We already resolved symlinks in validateAssetPath via EvalSymlinks,
	// so if we reach here with a symlink, EvalSymlinks followed it safely within
	// the working directory boundary. However, we still reject symlinks as a
	// defense-in-depth measure to prevent any edge cases.
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinks not allowed for asset paths: %s", assetPath)
	}

	// Open file
	file, err := os.Open(validatedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open asset %s: %w", assetPath, err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat asset %s: %w", assetPath, err)
	}

	fileName := fileInfo.Name()

	// Upload to GitLab's generic package registry
	// Package name: release-assets, version: tag name
	packageName := "release-assets"
	uploadOpts := &gitlab.PublishPackageFileOptions{
		Status: gitlab.Ptr(gitlab.PackageDefault),
	}

	_, _, err = client.GenericPackages.PublishPackageFile(
		projectID,
		packageName,
		tagName,
		fileName,
		file,
		uploadOpts,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upload asset: %w", err)
	}

	return &plugin.Artifact{
		Name: fileName,
		Path: fmt.Sprintf("packages/generic/%s/%s/%s", packageName, tagName, fileName),
		Type: "generic_package",
		Size: fileInfo.Size(),
	}, nil
}

// getClient creates a GitLab client.
func (p *GitLabPlugin) getClient(cfg *Config) (*gitlab.Client, error) {
	token := cfg.Token
	if token == "" {
		token = os.Getenv("GITLAB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GL_TOKEN")
	}

	if token == "" {
		return nil, fmt.Errorf("GitLab token is required (set GITLAB_TOKEN or configure token)")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}

	// Ensure base URL ends with /api/v4/
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	if !strings.HasSuffix(baseURL, "api/v4/") {
		baseURL += "api/v4/"
	}

	return gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
}

// parseConfig parses the plugin configuration.
func (p *GitLabPlugin) parseConfig(raw map[string]any) *Config {
	cfg := &Config{}

	if v, ok := raw["base_url"].(string); ok {
		cfg.BaseURL = v
	}
	if v, ok := raw["project_id"].(string); ok {
		cfg.ProjectID = v
	}
	if v, ok := raw["token"].(string); ok {
		cfg.Token = v
	}
	if v, ok := raw["name"].(string); ok {
		cfg.Name = v
	}
	if v, ok := raw["description"].(string); ok {
		cfg.Description = v
	}
	if v, ok := raw["ref"].(string); ok {
		cfg.Ref = v
	}
	if v, ok := raw["released_at"].(string); ok {
		cfg.ReleasedAt = v
	}

	// Parse milestones
	if v, ok := raw["milestones"].([]any); ok {
		for _, m := range v {
			if s, ok := m.(string); ok {
				cfg.Milestones = append(cfg.Milestones, s)
			}
		}
	}

	// Parse assets
	if v, ok := raw["assets"].([]any); ok {
		for _, a := range v {
			if s, ok := a.(string); ok {
				cfg.Assets = append(cfg.Assets, s)
			}
		}
	}

	// Parse asset links
	if v, ok := raw["asset_links"].([]any); ok {
		for _, linkRaw := range v {
			if linkMap, ok := linkRaw.(map[string]any); ok {
				link := AssetLink{}
				if name, ok := linkMap["name"].(string); ok {
					link.Name = name
				}
				if url, ok := linkMap["url"].(string); ok {
					link.URL = url
				}
				if fp, ok := linkMap["filepath"].(string); ok {
					link.FilePath = fp
				}
				if lt, ok := linkMap["link_type"].(string); ok {
					link.LinkType = lt
				}
				if link.Name != "" && link.URL != "" {
					cfg.AssetLinks = append(cfg.AssetLinks, link)
				}
			}
		}
	}

	return cfg
}

// Validate validates the plugin configuration.
func (p *GitLabPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	var errors []plugin.ValidationError

	// Token is required (either from config or environment)
	token := ""
	if v, ok := config["token"].(string); ok {
		token = v
	}
	if token == "" {
		token = os.Getenv("GITLAB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GL_TOKEN")
	}

	if token == "" {
		errors = append(errors, plugin.ValidationError{
			Field:   "token",
			Message: "GitLab token is required (set GITLAB_TOKEN env var or configure token)",
			Code:    "required",
		})
	}

	// Validate base_url if provided
	if baseURL, ok := config["base_url"].(string); ok && baseURL != "" {
		if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
			errors = append(errors, plugin.ValidationError{
				Field:   "base_url",
				Message: "base_url must start with http:// or https://",
				Code:    "format",
			})
		}
	}

	// Validate assets if provided
	if assets, ok := config["assets"].([]any); ok {
		for i, a := range assets {
			if _, ok := a.(string); !ok {
				errors = append(errors, plugin.ValidationError{
					Field:   fmt.Sprintf("assets[%d]", i),
					Message: "asset must be a string",
					Code:    "type",
				})
			}
		}
	}

	// Validate asset_links if provided
	if links, ok := config["asset_links"].([]any); ok {
		for i, linkRaw := range links {
			if linkMap, ok := linkRaw.(map[string]any); ok {
				if _, ok := linkMap["name"].(string); !ok {
					errors = append(errors, plugin.ValidationError{
						Field:   fmt.Sprintf("asset_links[%d].name", i),
						Message: "asset link name is required",
						Code:    "required",
					})
				}
				if _, ok := linkMap["url"].(string); !ok {
					errors = append(errors, plugin.ValidationError{
						Field:   fmt.Sprintf("asset_links[%d].url", i),
						Message: "asset link url is required",
						Code:    "required",
					})
				}
				if lt, ok := linkMap["link_type"].(string); ok {
					validTypes := map[string]bool{"other": true, "runbook": true, "image": true, "package": true}
					if !validTypes[lt] {
						errors = append(errors, plugin.ValidationError{
							Field:   fmt.Sprintf("asset_links[%d].link_type", i),
							Message: "link_type must be one of: other, runbook, image, package",
							Code:    "enum",
						})
					}
				}
			}
		}
	}

	// Validate milestones if provided
	if milestones, ok := config["milestones"].([]any); ok {
		for i, m := range milestones {
			if _, ok := m.(string); !ok {
				errors = append(errors, plugin.ValidationError{
					Field:   fmt.Sprintf("milestones[%d]", i),
					Message: "milestone must be a string",
					Code:    "type",
				})
			}
		}
	}

	return &plugin.ValidateResponse{
		Valid:  len(errors) == 0,
		Errors: errors,
	}, nil
}
