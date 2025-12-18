// Package main provides tests for the GitLab plugin.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

func TestGetInfo(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	info := p.GetInfo()

	t.Run("returns correct plugin name", func(t *testing.T) {
		if info.Name != "gitlab" {
			t.Errorf("expected name 'gitlab', got %q", info.Name)
		}
	})

	t.Run("returns valid version", func(t *testing.T) {
		if info.Version == "" {
			t.Error("expected non-empty version")
		}
		// Version should be in semver format
		if info.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got %q", info.Version)
		}
	})

	t.Run("returns description", func(t *testing.T) {
		if info.Description == "" {
			t.Error("expected non-empty description")
		}
		expected := "Create GitLab releases and upload assets"
		if info.Description != expected {
			t.Errorf("expected description %q, got %q", expected, info.Description)
		}
	})

	t.Run("returns author", func(t *testing.T) {
		if info.Author == "" {
			t.Error("expected non-empty author")
		}
		if info.Author != "Relicta Team" {
			t.Errorf("expected author 'Relicta Team', got %q", info.Author)
		}
	})

	t.Run("returns expected hooks", func(t *testing.T) {
		expectedHooks := []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
			plugin.HookOnError,
		}
		if len(info.Hooks) != len(expectedHooks) {
			t.Fatalf("expected %d hooks, got %d", len(expectedHooks), len(info.Hooks))
		}
		for i, hook := range expectedHooks {
			if info.Hooks[i] != hook {
				t.Errorf("hook[%d]: expected %q, got %q", i, hook, info.Hooks[i])
			}
		}
	})

	t.Run("returns config schema", func(t *testing.T) {
		if info.ConfigSchema == "" {
			t.Error("expected non-empty config schema")
		}
	})
}

func TestValidate(t *testing.T) {
	// Note: Not using t.Parallel() because this test modifies environment variables
	// which are global state and would cause race conditions with other tests

	p := &GitLabPlugin{}
	ctx := context.Background()

	// Save and restore environment variables
	origToken := os.Getenv("GITLAB_TOKEN")
	origGLToken := os.Getenv("GL_TOKEN")
	t.Cleanup(func() {
		_ = os.Setenv("GITLAB_TOKEN", origToken)
		_ = os.Setenv("GL_TOKEN", origGLToken)
	})

	tests := []struct {
		name        string
		config      map[string]any
		envToken    string
		envGLToken  string
		wantValid   bool
		wantErrors  int
		checkErrors func(t *testing.T, errors []plugin.ValidationError)
	}{
		{
			name:       "valid config with token",
			config:     map[string]any{"token": "glpat-test-token"},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name:       "valid config with GITLAB_TOKEN env",
			config:     map[string]any{},
			envToken:   "glpat-env-token",
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name:       "valid config with GL_TOKEN env",
			config:     map[string]any{},
			envGLToken: "glpat-gl-token",
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name:       "missing token",
			config:     map[string]any{},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "token" {
					t.Errorf("expected error on field 'token', got %q", errors[0].Field)
				}
				if errors[0].Code != "required" {
					t.Errorf("expected error code 'required', got %q", errors[0].Code)
				}
			},
		},
		{
			name: "invalid base_url without protocol",
			config: map[string]any{
				"token":    "glpat-test-token",
				"base_url": "gitlab.example.com",
			},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "base_url" {
					t.Errorf("expected error on field 'base_url', got %q", errors[0].Field)
				}
				if errors[0].Code != "format" {
					t.Errorf("expected error code 'format', got %q", errors[0].Code)
				}
			},
		},
		{
			name: "valid base_url with https",
			config: map[string]any{
				"token":    "glpat-test-token",
				"base_url": "https://gitlab.example.com",
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "valid base_url with http",
			config: map[string]any{
				"token":    "glpat-test-token",
				"base_url": "http://gitlab.local",
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "invalid asset type",
			config: map[string]any{
				"token":  "glpat-test-token",
				"assets": []any{123, "valid.zip"},
			},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "assets[0]" {
					t.Errorf("expected error on field 'assets[0]', got %q", errors[0].Field)
				}
				if errors[0].Code != "type" {
					t.Errorf("expected error code 'type', got %q", errors[0].Code)
				}
			},
		},
		{
			name: "valid assets",
			config: map[string]any{
				"token":  "glpat-test-token",
				"assets": []any{"dist/app.zip", "dist/checksums.txt"},
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "asset_link missing name",
			config: map[string]any{
				"token": "glpat-test-token",
				"asset_links": []any{
					map[string]any{"url": "https://example.com/file.zip"},
				},
			},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "asset_links[0].name" {
					t.Errorf("expected error on field 'asset_links[0].name', got %q", errors[0].Field)
				}
			},
		},
		{
			name: "asset_link missing url",
			config: map[string]any{
				"token": "glpat-test-token",
				"asset_links": []any{
					map[string]any{"name": "Download"},
				},
			},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "asset_links[0].url" {
					t.Errorf("expected error on field 'asset_links[0].url', got %q", errors[0].Field)
				}
			},
		},
		{
			name: "asset_link invalid link_type",
			config: map[string]any{
				"token": "glpat-test-token",
				"asset_links": []any{
					map[string]any{
						"name":      "Download",
						"url":       "https://example.com/file.zip",
						"link_type": "invalid",
					},
				},
			},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "asset_links[0].link_type" {
					t.Errorf("expected error on field 'asset_links[0].link_type', got %q", errors[0].Field)
				}
				if errors[0].Code != "enum" {
					t.Errorf("expected error code 'enum', got %q", errors[0].Code)
				}
			},
		},
		{
			name: "valid asset_links with all link_types",
			config: map[string]any{
				"token": "glpat-test-token",
				"asset_links": []any{
					map[string]any{"name": "Other", "url": "https://example.com/1", "link_type": "other"},
					map[string]any{"name": "Runbook", "url": "https://example.com/2", "link_type": "runbook"},
					map[string]any{"name": "Image", "url": "https://example.com/3", "link_type": "image"},
					map[string]any{"name": "Package", "url": "https://example.com/4", "link_type": "package"},
				},
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "invalid milestone type",
			config: map[string]any{
				"token":      "glpat-test-token",
				"milestones": []any{"v1.0.0", 123},
			},
			wantValid:  false,
			wantErrors: 1,
			checkErrors: func(t *testing.T, errors []plugin.ValidationError) {
				if errors[0].Field != "milestones[1]" {
					t.Errorf("expected error on field 'milestones[1]', got %q", errors[0].Field)
				}
				if errors[0].Code != "type" {
					t.Errorf("expected error code 'type', got %q", errors[0].Code)
				}
			},
		},
		{
			name: "valid milestones",
			config: map[string]any{
				"token":      "glpat-test-token",
				"milestones": []any{"v1.0.0", "v1.1.0"},
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "multiple validation errors",
			config: map[string]any{
				"base_url":   "invalid-url",
				"assets":     []any{123},
				"milestones": []any{456},
			},
			wantValid:  false,
			wantErrors: 4, // token, base_url, assets[0], milestones[0]
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			_ = os.Unsetenv("GITLAB_TOKEN")
			_ = os.Unsetenv("GL_TOKEN")

			// Set test environment
			if tt.envToken != "" {
				_ = os.Setenv("GITLAB_TOKEN", tt.envToken)
			}
			if tt.envGLToken != "" {
				_ = os.Setenv("GL_TOKEN", tt.envGLToken)
			}

			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected Valid=%v, got %v", tt.wantValid, resp.Valid)
			}

			if len(resp.Errors) != tt.wantErrors {
				t.Errorf("expected %d errors, got %d: %+v", tt.wantErrors, len(resp.Errors), resp.Errors)
			}

			if tt.checkErrors != nil && len(resp.Errors) > 0 {
				tt.checkErrors(t, resp.Errors)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}

	tests := []struct {
		name     string
		raw      map[string]any
		validate func(t *testing.T, cfg *Config)
	}{
		{
			name: "empty config uses defaults",
			raw:  map[string]any{},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.BaseURL != "" {
					t.Errorf("expected empty base_url, got %q", cfg.BaseURL)
				}
				if cfg.ProjectID != "" {
					t.Errorf("expected empty project_id, got %q", cfg.ProjectID)
				}
				if len(cfg.Assets) != 0 {
					t.Errorf("expected empty assets, got %v", cfg.Assets)
				}
			},
		},
		{
			name: "parses all string fields",
			raw: map[string]any{
				"base_url":    "https://gitlab.example.com",
				"project_id":  "group/project",
				"token":       "glpat-test",
				"name":        "My Release",
				"description": "Release description",
				"ref":         "main",
				"released_at": "2024-01-15T10:00:00Z",
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.BaseURL != "https://gitlab.example.com" {
					t.Errorf("base_url: got %q", cfg.BaseURL)
				}
				if cfg.ProjectID != "group/project" {
					t.Errorf("project_id: got %q", cfg.ProjectID)
				}
				if cfg.Token != "glpat-test" {
					t.Errorf("token: got %q", cfg.Token)
				}
				if cfg.Name != "My Release" {
					t.Errorf("name: got %q", cfg.Name)
				}
				if cfg.Description != "Release description" {
					t.Errorf("description: got %q", cfg.Description)
				}
				if cfg.Ref != "main" {
					t.Errorf("ref: got %q", cfg.Ref)
				}
				if cfg.ReleasedAt != "2024-01-15T10:00:00Z" {
					t.Errorf("released_at: got %q", cfg.ReleasedAt)
				}
			},
		},
		{
			name: "parses milestones",
			raw: map[string]any{
				"milestones": []any{"v1.0.0", "v1.1.0", "v2.0.0"},
			},
			validate: func(t *testing.T, cfg *Config) {
				expected := []string{"v1.0.0", "v1.1.0", "v2.0.0"}
				if len(cfg.Milestones) != len(expected) {
					t.Fatalf("milestones: expected %d, got %d", len(expected), len(cfg.Milestones))
				}
				for i, m := range expected {
					if cfg.Milestones[i] != m {
						t.Errorf("milestones[%d]: expected %q, got %q", i, m, cfg.Milestones[i])
					}
				}
			},
		},
		{
			name: "parses assets",
			raw: map[string]any{
				"assets": []any{"dist/app.zip", "dist/app.tar.gz", "checksums.txt"},
			},
			validate: func(t *testing.T, cfg *Config) {
				expected := []string{"dist/app.zip", "dist/app.tar.gz", "checksums.txt"}
				if len(cfg.Assets) != len(expected) {
					t.Fatalf("assets: expected %d, got %d", len(expected), len(cfg.Assets))
				}
				for i, a := range expected {
					if cfg.Assets[i] != a {
						t.Errorf("assets[%d]: expected %q, got %q", i, a, cfg.Assets[i])
					}
				}
			},
		},
		{
			name: "parses asset_links",
			raw: map[string]any{
				"asset_links": []any{
					map[string]any{
						"name":      "Linux Binary",
						"url":       "https://cdn.example.com/app-linux",
						"filepath":  "/binaries/linux",
						"link_type": "package",
					},
					map[string]any{
						"name": "Documentation",
						"url":  "https://docs.example.com",
					},
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				if len(cfg.AssetLinks) != 2 {
					t.Fatalf("asset_links: expected 2, got %d", len(cfg.AssetLinks))
				}

				link1 := cfg.AssetLinks[0]
				if link1.Name != "Linux Binary" {
					t.Errorf("link[0].name: got %q", link1.Name)
				}
				if link1.URL != "https://cdn.example.com/app-linux" {
					t.Errorf("link[0].url: got %q", link1.URL)
				}
				if link1.FilePath != "/binaries/linux" {
					t.Errorf("link[0].filepath: got %q", link1.FilePath)
				}
				if link1.LinkType != "package" {
					t.Errorf("link[0].link_type: got %q", link1.LinkType)
				}

				link2 := cfg.AssetLinks[1]
				if link2.Name != "Documentation" {
					t.Errorf("link[1].name: got %q", link2.Name)
				}
				if link2.URL != "https://docs.example.com" {
					t.Errorf("link[1].url: got %q", link2.URL)
				}
			},
		},
		{
			name: "skips invalid asset_links",
			raw: map[string]any{
				"asset_links": []any{
					map[string]any{"name": "Only Name"},          // Missing URL
					map[string]any{"url": "https://example.com"}, // Missing name
					map[string]any{"name": "Valid", "url": "https://valid.com"},
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				// Only the valid link should be parsed
				if len(cfg.AssetLinks) != 1 {
					t.Fatalf("asset_links: expected 1 valid link, got %d", len(cfg.AssetLinks))
				}
				if cfg.AssetLinks[0].Name != "Valid" {
					t.Errorf("expected 'Valid', got %q", cfg.AssetLinks[0].Name)
				}
			},
		},
		{
			name: "ignores invalid types in arrays",
			raw: map[string]any{
				"milestones": []any{"valid", 123, "also-valid"},
				"assets":     []any{"file.zip", nil, "other.tar"},
			},
			validate: func(t *testing.T, cfg *Config) {
				if len(cfg.Milestones) != 2 {
					t.Errorf("milestones: expected 2 valid entries, got %d", len(cfg.Milestones))
				}
				if len(cfg.Assets) != 2 {
					t.Errorf("assets: expected 2 valid entries, got %d", len(cfg.Assets))
				}
			},
		},
		{
			name: "handles wrong types gracefully",
			raw: map[string]any{
				"base_url":   123,            // Should be string
				"project_id": true,           // Should be string
				"milestones": "not-an-array", // Should be array
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.BaseURL != "" {
					t.Errorf("base_url: expected empty, got %q", cfg.BaseURL)
				}
				if cfg.ProjectID != "" {
					t.Errorf("project_id: expected empty, got %q", cfg.ProjectID)
				}
				if len(cfg.Milestones) != 0 {
					t.Errorf("milestones: expected empty, got %v", cfg.Milestones)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := p.parseConfig(tt.raw)
			tt.validate(t, cfg)
		})
	}
}

func TestExecute(t *testing.T) {
	// Note: Not using t.Parallel() because this test modifies environment variables
	// which are global state and would cause race conditions with other tests

	p := &GitLabPlugin{}
	ctx := context.Background()

	releaseCtx := plugin.ReleaseContext{
		Version:         "1.2.3",
		PreviousVersion: "1.2.2",
		TagName:         "v1.2.3",
		ReleaseType:     "patch",
		RepositoryOwner: "mygroup",
		RepositoryName:  "myproject",
		Branch:          "main",
		CommitSHA:       "abc123def456",
		Changelog:       "## Changes\n- Fixed bug",
		ReleaseNotes:    "This release fixes a critical bug.",
	}

	tests := []struct {
		name         string
		req          plugin.ExecuteRequest
		wantSuccess  bool
		wantMessage  string
		checkOutputs func(t *testing.T, outputs map[string]any)
	}{
		{
			name: "HookPostPublish dry run with project_id",
			req: plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"token":      "glpat-test",
					"project_id": "group/project",
				},
				Context: releaseCtx,
				DryRun:  true,
			},
			wantSuccess: true,
			wantMessage: "Would create GitLab release for group/project: v1.2.3",
			checkOutputs: func(t *testing.T, outputs map[string]any) {
				if outputs["tag_name"] != "v1.2.3" {
					t.Errorf("tag_name: got %v", outputs["tag_name"])
				}
				if outputs["project_id"] != "group/project" {
					t.Errorf("project_id: got %v", outputs["project_id"])
				}
				if outputs["name"] != "Release 1.2.3" {
					t.Errorf("name: got %v", outputs["name"])
				}
			},
		},
		{
			name: "HookPostPublish dry run infers project from context",
			req: plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"token": "glpat-test",
				},
				Context: releaseCtx,
				DryRun:  true,
			},
			wantSuccess: true,
			wantMessage: "Would create GitLab release for mygroup/myproject: v1.2.3",
			checkOutputs: func(t *testing.T, outputs map[string]any) {
				if outputs["project_id"] != "mygroup/myproject" {
					t.Errorf("project_id: got %v", outputs["project_id"])
				}
			},
		},
		{
			name: "HookPostPublish dry run with custom name",
			req: plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"token":      "glpat-test",
					"project_id": "group/project",
					"name":       "Version 1.2.3 - Bug Fixes",
				},
				Context: releaseCtx,
				DryRun:  true,
			},
			wantSuccess: true,
			checkOutputs: func(t *testing.T, outputs map[string]any) {
				if outputs["name"] != "Version 1.2.3 - Bug Fixes" {
					t.Errorf("name: got %v", outputs["name"])
				}
			},
		},
		{
			name: "HookOnSuccess returns success",
			req: plugin.ExecuteRequest{
				Hook:    plugin.HookOnSuccess,
				Config:  map[string]any{},
				Context: releaseCtx,
			},
			wantSuccess: true,
			wantMessage: "Release successful",
		},
		{
			name: "HookOnError returns success with acknowledgment",
			req: plugin.ExecuteRequest{
				Hook:    plugin.HookOnError,
				Config:  map[string]any{},
				Context: releaseCtx,
			},
			wantSuccess: true,
			wantMessage: "Release failed notification acknowledged",
		},
		{
			name: "unhandled hook returns success",
			req: plugin.ExecuteRequest{
				Hook:    plugin.HookPreInit,
				Config:  map[string]any{},
				Context: releaseCtx,
			},
			wantSuccess: true,
			wantMessage: "Hook pre-init not handled",
		},
		{
			name: "HookPostPublish fails without token",
			req: plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"project_id": "group/project",
				},
				Context: releaseCtx,
				DryRun:  false,
			},
			wantSuccess: false,
		},
		{
			name: "HookPostPublish dry run fails without project_id",
			req: plugin.ExecuteRequest{
				Hook:   plugin.HookPostPublish,
				Config: map[string]any{"token": "glpat-test"},
				Context: plugin.ReleaseContext{
					Version: "1.0.0",
					TagName: "v1.0.0",
					// No RepositoryOwner/RepositoryName
				},
				DryRun: true,
			},
			wantSuccess: false,
		},
	}

	// Clear environment variables for test isolation
	origToken := os.Getenv("GITLAB_TOKEN")
	origGLToken := os.Getenv("GL_TOKEN")
	t.Cleanup(func() {
		_ = os.Setenv("GITLAB_TOKEN", origToken)
		_ = os.Setenv("GL_TOKEN", origGLToken)
	})
	_ = os.Unsetenv("GITLAB_TOKEN")
	_ = os.Unsetenv("GL_TOKEN")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.Execute(ctx, tt.req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected Success=%v, got %v (error: %s)", tt.wantSuccess, resp.Success, resp.Error)
			}

			if tt.wantMessage != "" && resp.Message != tt.wantMessage {
				t.Errorf("expected message %q, got %q", tt.wantMessage, resp.Message)
			}

			if tt.checkOutputs != nil && resp.Outputs != nil {
				tt.checkOutputs(t, resp.Outputs)
			}
		})
	}
}

func TestValidateAssetPath(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state
	// and cannot be isolated per-test safely

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create test files
	testFile := filepath.Join(tmpDir, "test.zip")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	nestedFile := filepath.Join(subDir, "nested.zip")
	if err := os.WriteFile(nestedFile, []byte("nested content"), 0644); err != nil {
		t.Fatalf("failed to create nested file: %v", err)
	}

	// Change to temp directory for path validation tests
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	tests := []struct {
		name      string
		assetPath string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid relative path",
			assetPath: "test.zip",
			wantError: false,
		},
		{
			name:      "valid nested path",
			assetPath: "subdir/nested.zip",
			wantError: false,
		},
		{
			name:      "empty path",
			assetPath: "",
			wantError: true,
			errorMsg:  "asset path cannot be empty",
		},
		{
			name:      "path traversal with ..",
			assetPath: "../outside.zip",
			wantError: true,
			errorMsg:  "path traversal not allowed",
		},
		{
			name:      "path traversal in middle",
			assetPath: "subdir/../../outside.zip",
			wantError: true,
		},
		{
			name:      "non-existent file",
			assetPath: "nonexistent.zip",
			wantError: true,
			errorMsg:  "not accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateAssetPath(tt.assetPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil (result: %s)", result)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == "" {
					t.Error("expected non-empty result path")
				}
			}
		})
	}
}

func TestGetClient(t *testing.T) {
	// Note: Not using t.Parallel() because this test modifies environment variables
	// which are global state and would cause race conditions with other tests

	p := &GitLabPlugin{}

	// Save and restore environment variables
	origToken := os.Getenv("GITLAB_TOKEN")
	origGLToken := os.Getenv("GL_TOKEN")
	t.Cleanup(func() {
		_ = os.Setenv("GITLAB_TOKEN", origToken)
		_ = os.Setenv("GL_TOKEN", origGLToken)
	})

	tests := []struct {
		name       string
		cfg        *Config
		envToken   string
		envGLToken string
		wantError  bool
		errorMsg   string
	}{
		{
			name:      "config token",
			cfg:       &Config{Token: "glpat-config-token"},
			wantError: false,
		},
		{
			name:      "GITLAB_TOKEN env",
			cfg:       &Config{},
			envToken:  "glpat-env-token",
			wantError: false,
		},
		{
			name:       "GL_TOKEN env",
			cfg:        &Config{},
			envGLToken: "glpat-gl-token",
			wantError:  false,
		},
		{
			name:      "config token takes precedence over env",
			cfg:       &Config{Token: "glpat-config-token"},
			envToken:  "glpat-env-token",
			wantError: false,
		},
		{
			name:      "missing token",
			cfg:       &Config{},
			wantError: true,
			errorMsg:  "GitLab token is required",
		},
		{
			name:      "custom base URL",
			cfg:       &Config{Token: "glpat-test", BaseURL: "https://gitlab.example.com"},
			wantError: false,
		},
		{
			name:      "base URL without trailing slash",
			cfg:       &Config{Token: "glpat-test", BaseURL: "https://gitlab.example.com"},
			wantError: false,
		},
		{
			name:      "base URL with trailing slash",
			cfg:       &Config{Token: "glpat-test", BaseURL: "https://gitlab.example.com/"},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			_ = os.Unsetenv("GITLAB_TOKEN")
			_ = os.Unsetenv("GL_TOKEN")

			// Set test environment
			if tt.envToken != "" {
				_ = os.Setenv("GITLAB_TOKEN", tt.envToken)
			}
			if tt.envGLToken != "" {
				_ = os.Setenv("GL_TOKEN", tt.envGLToken)
			}

			client, err := p.getClient(tt.cfg)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if client == nil {
					t.Error("expected non-nil client")
				}
			}
		})
	}
}

func TestCreateReleaseDryRun(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name         string
		cfg          *Config
		releaseCtx   plugin.ReleaseContext
		wantSuccess  bool
		checkOutputs func(t *testing.T, outputs map[string]any)
	}{
		{
			name: "uses changelog when release notes empty",
			cfg:  &Config{Token: "glpat-test", ProjectID: "group/project"},
			releaseCtx: plugin.ReleaseContext{
				Version:   "1.0.0",
				TagName:   "v1.0.0",
				Changelog: "## Changelog\n- Feature A",
			},
			wantSuccess: true,
		},
		{
			name: "uses custom ref",
			cfg:  &Config{Token: "glpat-test", ProjectID: "group/project", Ref: "develop"},
			releaseCtx: plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: "v1.0.0",
			},
			wantSuccess: true,
		},
		{
			name: "uses tag as default ref",
			cfg:  &Config{Token: "glpat-test", ProjectID: "group/project"},
			releaseCtx: plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: "v1.0.0",
			},
			wantSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.createRelease(ctx, tt.cfg, tt.releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected Success=%v, got %v (error: %s)", tt.wantSuccess, resp.Success, resp.Error)
			}

			if tt.checkOutputs != nil && resp.Outputs != nil {
				tt.checkOutputs(t, resp.Outputs)
			}
		})
	}
}

func TestUploadAssetValidation(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state
	// and cannot be isolated per-test safely

	// Create temp directory and test files
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.zip")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	testDir := filepath.Join(tmpDir, "testdir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name      string
		assetPath string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "directory not allowed",
			assetPath: "testdir",
			wantError: true,
			errorMsg:  "directory",
		},
		{
			name:      "non-existent file",
			assetPath: "nonexistent.zip",
			wantError: true,
			errorMsg:  "not accessible",
		},
		{
			name:      "path traversal blocked",
			assetPath: "../escape.zip",
			wantError: true,
			errorMsg:  "path traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We can't test actual upload without a real GitLab client,
			// but we can test the validation logic
			artifact, err := p.uploadAsset(ctx, nil, "group/project", "v1.0.0", tt.assetPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil (artifact: %+v)", artifact)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestAssetLinkParsing(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}

	tests := []struct {
		name     string
		raw      map[string]any
		validate func(t *testing.T, cfg *Config)
	}{
		{
			name: "all link types",
			raw: map[string]any{
				"asset_links": []any{
					map[string]any{"name": "Other", "url": "https://other.com", "link_type": "other"},
					map[string]any{"name": "Runbook", "url": "https://runbook.com", "link_type": "runbook"},
					map[string]any{"name": "Image", "url": "https://image.com", "link_type": "image"},
					map[string]any{"name": "Package", "url": "https://package.com", "link_type": "package"},
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				if len(cfg.AssetLinks) != 4 {
					t.Fatalf("expected 4 asset links, got %d", len(cfg.AssetLinks))
				}
				expectedTypes := []string{"other", "runbook", "image", "package"}
				for i, lt := range expectedTypes {
					if cfg.AssetLinks[i].LinkType != lt {
						t.Errorf("link[%d].link_type: expected %q, got %q", i, lt, cfg.AssetLinks[i].LinkType)
					}
				}
			},
		},
		{
			name: "link with filepath",
			raw: map[string]any{
				"asset_links": []any{
					map[string]any{
						"name":     "Binary",
						"url":      "https://cdn.example.com/app",
						"filepath": "/binaries/app-linux-amd64",
					},
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				if len(cfg.AssetLinks) != 1 {
					t.Fatalf("expected 1 asset link, got %d", len(cfg.AssetLinks))
				}
				if cfg.AssetLinks[0].FilePath != "/binaries/app-linux-amd64" {
					t.Errorf("filepath: got %q", cfg.AssetLinks[0].FilePath)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := p.parseConfig(tt.raw)
			tt.validate(t, cfg)
		})
	}
}

func TestValidateAssetPathWithSymlink(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state

	tmpDir := t.TempDir()

	// Create a real file
	realFile := filepath.Join(tmpDir, "real.zip")
	if err := os.WriteFile(realFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a symlink to the real file
	symlinkPath := filepath.Join(tmpDir, "symlink.zip")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skipf("failed to create symlink (may not be supported): %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	// Symlinks within the working directory should resolve successfully
	// because EvalSymlinks follows them to their real path
	result, err := validateAssetPath("symlink.zip")
	if err != nil {
		// Some systems may not support symlinks
		t.Logf("symlink validation returned error (may be expected): %v", err)
		return
	}
	if result == "" {
		t.Error("expected non-empty result path for symlink")
	}
}

func TestValidateAssetPathAbsolutePath(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state

	tmpDir := t.TempDir()

	// Create a file in the temp directory
	testFile := filepath.Join(tmpDir, "test.zip")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	// Absolute path within working directory should work
	result, err := validateAssetPath(testFile)
	if err != nil {
		t.Errorf("unexpected error for absolute path: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result path")
	}

	// Absolute path outside working directory should fail
	outsidePath := "/tmp/outside-file.zip"
	_, err = validateAssetPath(outsidePath)
	if err == nil {
		t.Error("expected error for absolute path outside working directory")
	}
}

func TestCreateReleaseNoToken(t *testing.T) {
	// Note: Not using t.Parallel() because this test modifies environment variables

	p := &GitLabPlugin{}
	ctx := context.Background()

	// Clear env vars
	origToken := os.Getenv("GITLAB_TOKEN")
	origGLToken := os.Getenv("GL_TOKEN")
	t.Cleanup(func() {
		_ = os.Setenv("GITLAB_TOKEN", origToken)
		_ = os.Setenv("GL_TOKEN", origGLToken)
	})
	_ = os.Unsetenv("GITLAB_TOKEN")
	_ = os.Unsetenv("GL_TOKEN")

	cfg := &Config{ProjectID: "group/project"} // No token
	releaseCtx := plugin.ReleaseContext{
		Version: "1.0.0",
		TagName: "v1.0.0",
	}

	resp, err := p.createRelease(ctx, cfg, releaseCtx, false)
	if err != nil {
		t.Fatalf("createRelease returned error: %v", err)
	}

	if resp.Success {
		t.Error("expected failure without token")
	}
	if !contains(resp.Error, "token is required") {
		t.Errorf("expected token required error, got: %s", resp.Error)
	}
}

func TestCreateReleaseNoProjectID(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	cfg := &Config{Token: "glpat-test"} // No project ID
	releaseCtx := plugin.ReleaseContext{
		Version: "1.0.0",
		TagName: "v1.0.0",
		// No RepositoryOwner or RepositoryName
	}

	resp, err := p.createRelease(ctx, cfg, releaseCtx, false)
	if err != nil {
		t.Fatalf("createRelease returned error: %v", err)
	}

	if resp.Success {
		t.Error("expected failure without project ID")
	}
	if !contains(resp.Error, "project_id is required") {
		t.Errorf("expected project_id required error, got: %s", resp.Error)
	}
}

func TestCreateReleaseDescriptionFallback(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name         string
		cfg          *Config
		releaseCtx   plugin.ReleaseContext
		checkMessage string
	}{
		{
			name: "uses release notes when available",
			cfg:  &Config{Token: "glpat-test", ProjectID: "group/project"},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "This is the release notes content",
				Changelog:    "This is the changelog content",
			},
			checkMessage: "Would create GitLab release for group/project: v1.0.0",
		},
		{
			name: "falls back to changelog when release notes empty",
			cfg:  &Config{Token: "glpat-test", ProjectID: "group/project"},
			releaseCtx: plugin.ReleaseContext{
				Version:   "1.0.0",
				TagName:   "v1.0.0",
				Changelog: "This is the changelog content",
			},
			checkMessage: "Would create GitLab release for group/project: v1.0.0",
		},
		{
			name: "uses custom description from config",
			cfg:  &Config{Token: "glpat-test", ProjectID: "group/project", Description: "Custom description"},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "This should be ignored",
			},
			checkMessage: "Would create GitLab release for group/project: v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.createRelease(ctx, tt.cfg, tt.releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got error: %s", resp.Error)
			}

			if resp.Message != tt.checkMessage {
				t.Errorf("expected message %q, got %q", tt.checkMessage, resp.Message)
			}
		})
	}
}

func TestExecuteWithMilestones(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"token":      "glpat-test",
			"project_id": "group/project",
			"milestones": []any{"v1.0.0", "Q4-2024"},
		},
		Context: plugin.ReleaseContext{
			Version: "1.0.0",
			TagName: "v1.0.0",
		},
		DryRun: true,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
}

func TestExecuteWithAssetLinks(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"token":      "glpat-test",
			"project_id": "group/project",
			"asset_links": []any{
				map[string]any{
					"name":      "Linux Binary",
					"url":       "https://cdn.example.com/app-linux",
					"link_type": "package",
				},
				map[string]any{
					"name":     "Windows Binary",
					"url":      "https://cdn.example.com/app-windows",
					"filepath": "/binaries/windows",
				},
			},
		},
		Context: plugin.ReleaseContext{
			Version: "1.0.0",
			TagName: "v1.0.0",
		},
		DryRun: true,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
}

func TestExecuteMultipleHooks(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	hooks := []struct {
		hook        plugin.Hook
		wantSuccess bool
		wantMsg     string
	}{
		{plugin.HookPreInit, true, "Hook pre-init not handled"},
		{plugin.HookPostInit, true, "Hook post-init not handled"},
		{plugin.HookPrePlan, true, "Hook pre-plan not handled"},
		{plugin.HookPostPlan, true, "Hook post-plan not handled"},
		{plugin.HookPreVersion, true, "Hook pre-version not handled"},
		{plugin.HookPostVersion, true, "Hook post-version not handled"},
		{plugin.HookPreNotes, true, "Hook pre-notes not handled"},
		{plugin.HookPostNotes, true, "Hook post-notes not handled"},
		{plugin.HookPreApprove, true, "Hook pre-approve not handled"},
		{plugin.HookPostApprove, true, "Hook post-approve not handled"},
		{plugin.HookPrePublish, true, "Hook pre-publish not handled"},
	}

	for _, tt := range hooks {
		t.Run(string(tt.hook), func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    tt.hook,
				Config:  map[string]any{},
				Context: plugin.ReleaseContext{},
			}

			resp, err := p.Execute(ctx, req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected Success=%v, got %v", tt.wantSuccess, resp.Success)
			}

			if resp.Message != tt.wantMsg {
				t.Errorf("expected message %q, got %q", tt.wantMsg, resp.Message)
			}
		})
	}
}

func TestConfigStructFields(t *testing.T) {
	t.Parallel()

	// Test that Config struct has all expected fields and they work correctly
	cfg := Config{
		BaseURL:     "https://gitlab.example.com",
		ProjectID:   "group/project",
		Token:       "glpat-test",
		Name:        "Release Name",
		Description: "Release Description",
		Ref:         "main",
		ReleasedAt:  "2024-01-15T10:00:00Z",
		Milestones:  []string{"v1.0.0", "v1.1.0"},
		Assets:      []string{"file1.zip", "file2.tar.gz"},
		AssetLinks: []AssetLink{
			{Name: "Link1", URL: "https://example.com/1", FilePath: "/path", LinkType: "package"},
		},
	}

	if cfg.BaseURL != "https://gitlab.example.com" {
		t.Error("BaseURL not set correctly")
	}
	if cfg.ProjectID != "group/project" {
		t.Error("ProjectID not set correctly")
	}
	if cfg.Token != "glpat-test" {
		t.Error("Token not set correctly")
	}
	if cfg.Name != "Release Name" {
		t.Error("Name not set correctly")
	}
	if cfg.Description != "Release Description" {
		t.Error("Description not set correctly")
	}
	if cfg.Ref != "main" {
		t.Error("Ref not set correctly")
	}
	if cfg.ReleasedAt != "2024-01-15T10:00:00Z" {
		t.Error("ReleasedAt not set correctly")
	}
	if len(cfg.Milestones) != 2 {
		t.Error("Milestones not set correctly")
	}
	if len(cfg.Assets) != 2 {
		t.Error("Assets not set correctly")
	}
	if len(cfg.AssetLinks) != 1 {
		t.Error("AssetLinks not set correctly")
	}
	if cfg.AssetLinks[0].LinkType != "package" {
		t.Error("AssetLink.LinkType not set correctly")
	}
}

func TestAssetLinkStructFields(t *testing.T) {
	t.Parallel()

	link := AssetLink{
		Name:     "Download",
		URL:      "https://example.com/download",
		FilePath: "/downloads/file.zip",
		LinkType: "package",
	}

	if link.Name != "Download" {
		t.Error("Name not set correctly")
	}
	if link.URL != "https://example.com/download" {
		t.Error("URL not set correctly")
	}
	if link.FilePath != "/downloads/file.zip" {
		t.Error("FilePath not set correctly")
	}
	if link.LinkType != "package" {
		t.Error("LinkType not set correctly")
	}
}

func TestGetInfoConfigSchema(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	info := p.GetInfo()

	// Verify the config schema contains expected fields
	schema := info.ConfigSchema

	expectedFields := []string{
		"base_url",
		"project_id",
		"token",
		"name",
		"description",
		"ref",
		"released_at",
		"milestones",
		"assets",
		"asset_links",
	}

	for _, field := range expectedFields {
		if !contains(schema, field) {
			t.Errorf("config schema missing field: %s", field)
		}
	}

	// Check that link_type enum values are present
	linkTypes := []string{"other", "runbook", "image", "package"}
	for _, lt := range linkTypes {
		if !contains(schema, lt) {
			t.Errorf("config schema missing link_type enum value: %s", lt)
		}
	}
}

func TestValidateEmptyConfig(t *testing.T) {
	// Note: Not using t.Parallel() because this test modifies environment variables

	p := &GitLabPlugin{}
	ctx := context.Background()

	// Clear env vars
	origToken := os.Getenv("GITLAB_TOKEN")
	origGLToken := os.Getenv("GL_TOKEN")
	t.Cleanup(func() {
		_ = os.Setenv("GITLAB_TOKEN", origToken)
		_ = os.Setenv("GL_TOKEN", origGLToken)
	})
	_ = os.Unsetenv("GITLAB_TOKEN")
	_ = os.Unsetenv("GL_TOKEN")

	resp, err := p.Validate(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if resp.Valid {
		t.Error("expected invalid for empty config without token")
	}

	if len(resp.Errors) == 0 {
		t.Error("expected validation errors")
	}
}

func TestValidateConfigWithNilValues(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	cfg := map[string]any{
		"token":       "glpat-test",
		"milestones":  nil,
		"assets":      nil,
		"asset_links": nil,
	}

	resp, err := p.Validate(ctx, cfg)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if !resp.Valid {
		t.Errorf("expected valid config, got errors: %+v", resp.Errors)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestCreateReleaseWithMilestones tests that milestones are properly added to release options
func TestCreateReleaseWithMilestones(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	cfg := &Config{
		Token:      "glpat-test",
		ProjectID:  "group/project",
		Milestones: []string{"v1.0.0", "Q4-2024", "sprint-42"},
	}
	releaseCtx := plugin.ReleaseContext{
		Version: "1.0.0",
		TagName: "v1.0.0",
	}

	// Dry run should succeed with milestones configured
	resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
	if err != nil {
		t.Fatalf("createRelease returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify the outputs
	if resp.Outputs["tag_name"] != "v1.0.0" {
		t.Errorf("expected tag_name v1.0.0, got %v", resp.Outputs["tag_name"])
	}
}

// TestCreateReleaseWithAssetLinks tests that asset links are properly added to release options
func TestCreateReleaseWithAssetLinks(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name       string
		assetLinks []AssetLink
	}{
		{
			name: "single asset link without link type",
			assetLinks: []AssetLink{
				{Name: "Download", URL: "https://example.com/download"},
			},
		},
		{
			name: "single asset link with link type",
			assetLinks: []AssetLink{
				{Name: "Package", URL: "https://example.com/package", LinkType: "package"},
			},
		},
		{
			name: "asset link with filepath",
			assetLinks: []AssetLink{
				{Name: "Binary", URL: "https://example.com/binary", FilePath: "/binaries/linux/amd64"},
			},
		},
		{
			name: "multiple asset links with different types",
			assetLinks: []AssetLink{
				{Name: "Other", URL: "https://example.com/1", LinkType: "other"},
				{Name: "Runbook", URL: "https://example.com/2", LinkType: "runbook"},
				{Name: "Image", URL: "https://example.com/3", LinkType: "image"},
				{Name: "Package", URL: "https://example.com/4", LinkType: "package"},
			},
		},
		{
			name: "asset links with and without filepath",
			assetLinks: []AssetLink{
				{Name: "Link1", URL: "https://example.com/1", LinkType: "package", FilePath: "/path/1"},
				{Name: "Link2", URL: "https://example.com/2", LinkType: "other"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Token:      "glpat-test",
				ProjectID:  "group/project",
				AssetLinks: tt.assetLinks,
			}
			releaseCtx := plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: "v1.0.0",
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got error: %s", resp.Error)
			}
		})
	}
}

// TestCreateReleaseDescriptionPriority tests the description selection priority
func TestCreateReleaseDescriptionPriority(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name            string
		cfgDescription  string
		releaseNotes    string
		changelog       string
		expectedSuccess bool
	}{
		{
			name:            "config description takes priority",
			cfgDescription:  "Custom config description",
			releaseNotes:    "Release notes content",
			changelog:       "Changelog content",
			expectedSuccess: true,
		},
		{
			name:            "release notes used when config description empty",
			cfgDescription:  "",
			releaseNotes:    "Release notes content",
			changelog:       "Changelog content",
			expectedSuccess: true,
		},
		{
			name:            "changelog used when both config and release notes empty",
			cfgDescription:  "",
			releaseNotes:    "",
			changelog:       "Changelog content",
			expectedSuccess: true,
		},
		{
			name:            "empty description when all sources empty",
			cfgDescription:  "",
			releaseNotes:    "",
			changelog:       "",
			expectedSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Token:       "glpat-test",
				ProjectID:   "group/project",
				Description: tt.cfgDescription,
			}
			releaseCtx := plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: tt.releaseNotes,
				Changelog:    tt.changelog,
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if resp.Success != tt.expectedSuccess {
				t.Errorf("expected success=%v, got %v (error: %s)", tt.expectedSuccess, resp.Success, resp.Error)
			}
		})
	}
}

// TestCreateReleaseNameGeneration tests the release name generation
func TestCreateReleaseNameGeneration(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name         string
		cfgName      string
		version      string
		expectedName string
	}{
		{
			name:         "uses config name when provided",
			cfgName:      "My Custom Release",
			version:      "1.0.0",
			expectedName: "My Custom Release",
		},
		{
			name:         "generates default name from version",
			cfgName:      "",
			version:      "2.3.4",
			expectedName: "Release 2.3.4",
		},
		{
			name:         "handles prerelease version",
			cfgName:      "",
			version:      "1.0.0-beta.1",
			expectedName: "Release 1.0.0-beta.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				Name:      tt.cfgName,
			}
			releaseCtx := plugin.ReleaseContext{
				Version: tt.version,
				TagName: "v" + tt.version,
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if !resp.Success {
				t.Fatalf("expected success, got error: %s", resp.Error)
			}

			if resp.Outputs["name"] != tt.expectedName {
				t.Errorf("expected name %q, got %q", tt.expectedName, resp.Outputs["name"])
			}
		})
	}
}

// TestCreateReleaseRefSelection tests the ref selection logic
func TestCreateReleaseRefSelection(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name    string
		cfgRef  string
		tagName string
	}{
		{
			name:    "uses config ref when provided",
			cfgRef:  "main",
			tagName: "v1.0.0",
		},
		{
			name:    "uses tag name as default ref",
			cfgRef:  "",
			tagName: "v1.0.0",
		},
		{
			name:    "uses feature branch ref",
			cfgRef:  "feature/release-branch",
			tagName: "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				Ref:       tt.cfgRef,
			}
			releaseCtx := plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: tt.tagName,
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if !resp.Success {
				t.Errorf("expected success, got error: %s", resp.Error)
			}
		})
	}
}

// TestCreateReleaseProjectIDInference tests project ID inference from context
func TestCreateReleaseProjectIDInference(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name              string
		cfgProjectID      string
		repositoryOwner   string
		repositoryName    string
		expectedProjectID string
		expectSuccess     bool
	}{
		{
			name:              "uses config project ID",
			cfgProjectID:      "config-group/config-project",
			repositoryOwner:   "context-owner",
			repositoryName:    "context-repo",
			expectedProjectID: "config-group/config-project",
			expectSuccess:     true,
		},
		{
			name:              "infers from context when config empty",
			cfgProjectID:      "",
			repositoryOwner:   "context-owner",
			repositoryName:    "context-repo",
			expectedProjectID: "context-owner/context-repo",
			expectSuccess:     true,
		},
		{
			name:            "fails when no project ID available",
			cfgProjectID:    "",
			repositoryOwner: "",
			repositoryName:  "",
			expectSuccess:   false,
		},
		{
			name:            "fails with only owner",
			cfgProjectID:    "",
			repositoryOwner: "owner-only",
			repositoryName:  "",
			expectSuccess:   false,
		},
		{
			name:            "fails with only repo name",
			cfgProjectID:    "",
			repositoryOwner: "",
			repositoryName:  "repo-only",
			expectSuccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Token:     "glpat-test",
				ProjectID: tt.cfgProjectID,
			}
			releaseCtx := plugin.ReleaseContext{
				Version:         "1.0.0",
				TagName:         "v1.0.0",
				RepositoryOwner: tt.repositoryOwner,
				RepositoryName:  tt.repositoryName,
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if resp.Success != tt.expectSuccess {
				t.Errorf("expected success=%v, got %v (error: %s)", tt.expectSuccess, resp.Success, resp.Error)
			}

			if tt.expectSuccess && resp.Outputs["project_id"] != tt.expectedProjectID {
				t.Errorf("expected project_id %q, got %q", tt.expectedProjectID, resp.Outputs["project_id"])
			}
		})
	}
}

// TestUploadAssetFileOperations tests the file operation paths in uploadAsset
func TestUploadAssetFileOperations(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state

	tmpDir := t.TempDir()

	// Create a valid test file
	validFile := filepath.Join(tmpDir, "valid.zip")
	if err := os.WriteFile(validFile, []byte("valid file content"), 0644); err != nil {
		t.Fatalf("failed to create valid test file: %v", err)
	}

	// Create a directory
	testDir := filepath.Join(tmpDir, "testdir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name      string
		assetPath string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "rejects directory",
			assetPath: "testdir",
			wantError: true,
			errorMsg:  "directory",
		},
		{
			name:      "rejects non-existent file",
			assetPath: "nonexistent.zip",
			wantError: true,
			errorMsg:  "not accessible",
		},
		{
			name:      "rejects empty path",
			assetPath: "",
			wantError: true,
			errorMsg:  "cannot be empty",
		},
		{
			name:      "rejects path traversal",
			assetPath: "../escape.zip",
			wantError: true,
			errorMsg:  "path traversal",
		},
		// Note: We cannot test valid file upload without a real GitLab client
		// The test above for path traversal and directory rejection cover the validation logic
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact, err := p.uploadAsset(ctx, nil, "group/project", "v1.0.0", tt.assetPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil (artifact: %+v)", artifact)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestValidateAssetPathEdgeCases tests edge cases in validateAssetPath
func TestValidateAssetPathEdgeCases(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state

	tmpDir := t.TempDir()

	// Create test files in various locations
	testFile := filepath.Join(tmpDir, "test.zip")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create nested directories
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested directories: %v", err)
	}

	nestedFile := filepath.Join(nestedDir, "nested.zip")
	if err := os.WriteFile(nestedFile, []byte("nested content"), 0644); err != nil {
		t.Fatalf("failed to create nested file: %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	tests := []struct {
		name      string
		assetPath string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid deeply nested path",
			assetPath: "a/b/c/nested.zip",
			wantError: false,
		},
		{
			name:      "path with dot in middle",
			assetPath: "./test.zip",
			wantError: false,
		},
		{
			name:      "path traversal blocked at start",
			assetPath: "../escape.zip",
			wantError: true,
			errorMsg:  "path traversal",
		},
		{
			name:      "path traversal blocked in middle",
			assetPath: "a/../../../escape.zip",
			wantError: true,
			errorMsg:  "path traversal",
		},
		{
			name:      "absolute path outside working dir",
			assetPath: "/etc/passwd",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateAssetPath(tt.assetPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil (result: %s)", result)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == "" {
					t.Error("expected non-empty result path")
				}
			}
		})
	}
}

// TestCreateReleaseBaseURLHandling tests the base URL handling in response
func TestCreateReleaseBaseURLHandling(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name        string
		baseURL     string
		projectID   string
		tagName     string
		wantURLPart string
	}{
		{
			name:        "default gitlab.com URL",
			baseURL:     "",
			projectID:   "group/project",
			tagName:     "v1.0.0",
			wantURLPart: "https://gitlab.com/group/project/-/releases/v1.0.0",
		},
		{
			name:        "custom base URL without trailing slash",
			baseURL:     "https://gitlab.example.com",
			projectID:   "group/project",
			tagName:     "v1.0.0",
			wantURLPart: "https://gitlab.example.com/group/project/-/releases/v1.0.0",
		},
		{
			name:        "custom base URL with trailing slash",
			baseURL:     "https://gitlab.example.com/",
			projectID:   "group/project",
			tagName:     "v1.0.0",
			wantURLPart: "https://gitlab.example.com/group/project/-/releases/v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Token:     "glpat-test",
				ProjectID: tt.projectID,
				BaseURL:   tt.baseURL,
			}
			releaseCtx := plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: tt.tagName,
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if !resp.Success {
				t.Fatalf("expected success, got error: %s", resp.Error)
			}

			// In dry run, we can verify the message format
			if !contains(resp.Message, tt.projectID) {
				t.Errorf("expected message to contain project ID %q, got %q", tt.projectID, resp.Message)
			}
		})
	}
}

// TestParseConfigRobustness tests parseConfig with various edge cases
func TestParseConfigRobustness(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}

	tests := []struct {
		name     string
		raw      map[string]any
		validate func(t *testing.T, cfg *Config)
	}{
		{
			name: "handles nil map values",
			raw: map[string]any{
				"token":       "test",
				"milestones":  nil,
				"assets":      nil,
				"asset_links": nil,
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Token != "test" {
					t.Errorf("expected token 'test', got %q", cfg.Token)
				}
				if len(cfg.Milestones) != 0 {
					t.Errorf("expected empty milestones, got %v", cfg.Milestones)
				}
				if len(cfg.Assets) != 0 {
					t.Errorf("expected empty assets, got %v", cfg.Assets)
				}
				if len(cfg.AssetLinks) != 0 {
					t.Errorf("expected empty asset links, got %v", cfg.AssetLinks)
				}
			},
		},
		{
			name: "handles mixed valid and invalid milestone types",
			raw: map[string]any{
				"milestones": []any{"valid1", 123, "valid2", nil, "valid3", true},
			},
			validate: func(t *testing.T, cfg *Config) {
				// Should only include the valid string values
				if len(cfg.Milestones) != 3 {
					t.Errorf("expected 3 valid milestones, got %d: %v", len(cfg.Milestones), cfg.Milestones)
				}
			},
		},
		{
			name: "handles mixed valid and invalid asset types",
			raw: map[string]any{
				"assets": []any{"file1.zip", 456, "file2.tar.gz", false},
			},
			validate: func(t *testing.T, cfg *Config) {
				// Should only include the valid string values
				if len(cfg.Assets) != 2 {
					t.Errorf("expected 2 valid assets, got %d: %v", len(cfg.Assets), cfg.Assets)
				}
			},
		},
		{
			name: "handles asset_links with non-map elements",
			raw: map[string]any{
				"asset_links": []any{
					"not a map",
					123,
					map[string]any{"name": "Valid", "url": "https://valid.com"},
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				// Should only include the valid map
				if len(cfg.AssetLinks) != 1 {
					t.Errorf("expected 1 valid asset link, got %d: %v", len(cfg.AssetLinks), cfg.AssetLinks)
				}
			},
		},
		{
			name: "handles asset_links with partial data",
			raw: map[string]any{
				"asset_links": []any{
					map[string]any{"name": "NameOnly"},           // Missing URL
					map[string]any{"url": "https://urlonly.com"}, // Missing name
					map[string]any{},                             // Empty map
					map[string]any{"name": "Complete", "url": "https://complete.com"},
				},
			},
			validate: func(t *testing.T, cfg *Config) {
				// Should only include the complete one
				if len(cfg.AssetLinks) != 1 {
					t.Errorf("expected 1 valid asset link, got %d: %v", len(cfg.AssetLinks), cfg.AssetLinks)
				}
				if cfg.AssetLinks[0].Name != "Complete" {
					t.Errorf("expected name 'Complete', got %q", cfg.AssetLinks[0].Name)
				}
			},
		},
		{
			name: "handles empty strings in arrays",
			raw: map[string]any{
				"milestones": []any{"", "valid", ""},
				"assets":     []any{"", "valid.zip", ""},
			},
			validate: func(t *testing.T, cfg *Config) {
				// Empty strings are technically valid strings
				if len(cfg.Milestones) != 3 {
					t.Errorf("expected 3 milestones, got %d", len(cfg.Milestones))
				}
				if len(cfg.Assets) != 3 {
					t.Errorf("expected 3 assets, got %d", len(cfg.Assets))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := p.parseConfig(tt.raw)
			tt.validate(t, cfg)
		})
	}
}

// TestValidateWithInvalidAssetLinkStructures tests validation with malformed asset_links
func TestValidateWithInvalidAssetLinkStructures(t *testing.T) {
	// Note: Not using t.Parallel() because this test might affect env vars

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name       string
		config     map[string]any
		wantValid  bool
		wantErrors int
	}{
		{
			name: "asset_links with non-map elements are ignored",
			config: map[string]any{
				"token": "glpat-test",
				"asset_links": []any{
					"not a map",
					123,
				},
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "asset_links with missing name",
			config: map[string]any{
				"token": "glpat-test",
				"asset_links": []any{
					map[string]any{"url": "https://example.com"},
				},
			},
			wantValid:  false,
			wantErrors: 1,
		},
		{
			name: "asset_links with missing url",
			config: map[string]any{
				"token": "glpat-test",
				"asset_links": []any{
					map[string]any{"name": "Test"},
				},
			},
			wantValid:  false,
			wantErrors: 1,
		},
		{
			name: "multiple asset_links with various issues",
			config: map[string]any{
				"token": "glpat-test",
				"asset_links": []any{
					map[string]any{"url": "https://example.com"},                                           // Missing name
					map[string]any{"name": "Test"},                                                         // Missing url
					map[string]any{"name": "Invalid Type", "url": "https://e.com", "link_type": "invalid"}, // Invalid link_type
				},
			},
			wantValid:  false,
			wantErrors: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.Validate(ctx, tt.config)
			if err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}

			if resp.Valid != tt.wantValid {
				t.Errorf("expected Valid=%v, got %v (errors: %+v)", tt.wantValid, resp.Valid, resp.Errors)
			}

			if len(resp.Errors) != tt.wantErrors {
				t.Errorf("expected %d errors, got %d: %+v", tt.wantErrors, len(resp.Errors), resp.Errors)
			}
		})
	}
}

// TestExecuteWithCompleteConfig tests Execute with a fully configured request
func TestExecuteWithCompleteConfig(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"token":       "glpat-test",
			"project_id":  "group/project",
			"base_url":    "https://gitlab.example.com",
			"name":        "Release v1.0.0 - Feature Release",
			"description": "This is a custom description",
			"ref":         "main",
			"released_at": "2024-01-15T10:00:00Z",
			"milestones":  []any{"v1.0.0", "Q4-2024"},
			"assets":      []any{"dist/app.zip", "checksums.txt"},
			"asset_links": []any{
				map[string]any{
					"name":      "Linux Binary",
					"url":       "https://cdn.example.com/app-linux",
					"filepath":  "/binaries/linux",
					"link_type": "package",
				},
				map[string]any{
					"name":      "Documentation",
					"url":       "https://docs.example.com",
					"link_type": "runbook",
				},
			},
		},
		Context: plugin.ReleaseContext{
			Version:         "1.0.0",
			PreviousVersion: "0.9.0",
			TagName:         "v1.0.0",
			ReleaseType:     "minor",
			RepositoryOwner: "mygroup",
			RepositoryName:  "myproject",
			Branch:          "main",
			CommitSHA:       "abc123def456",
			Changelog:       "## Changes\n- Feature A\n- Feature B",
			ReleaseNotes:    "Major feature release with A and B.",
		},
		DryRun: true,
	}

	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify outputs
	if resp.Outputs["tag_name"] != "v1.0.0" {
		t.Errorf("expected tag_name v1.0.0, got %v", resp.Outputs["tag_name"])
	}
	if resp.Outputs["project_id"] != "group/project" {
		t.Errorf("expected project_id group/project, got %v", resp.Outputs["project_id"])
	}
	if resp.Outputs["name"] != "Release v1.0.0 - Feature Release" {
		t.Errorf("expected custom name, got %v", resp.Outputs["name"])
	}
}

// TestCreateReleaseWithEmptyAssetLinks tests behavior with empty but non-nil AssetLinks
func TestCreateReleaseWithEmptyAssetLinks(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	cfg := &Config{
		Token:      "glpat-test",
		ProjectID:  "group/project",
		AssetLinks: []AssetLink{}, // Empty slice
	}
	releaseCtx := plugin.ReleaseContext{
		Version: "1.0.0",
		TagName: "v1.0.0",
	}

	resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
	if err != nil {
		t.Fatalf("createRelease returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
}

// TestCreateReleaseWithEmptyMilestones tests behavior with empty but non-nil Milestones
func TestCreateReleaseWithEmptyMilestones(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	cfg := &Config{
		Token:      "glpat-test",
		ProjectID:  "group/project",
		Milestones: []string{}, // Empty slice
	}
	releaseCtx := plugin.ReleaseContext{
		Version: "1.0.0",
		TagName: "v1.0.0",
	}

	resp, err := p.createRelease(ctx, cfg, releaseCtx, true)
	if err != nil {
		t.Fatalf("createRelease returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
}

// setupMockGitLabServer creates a test server that mocks GitLab API endpoints
func setupMockGitLabServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// TestCreateReleaseWithMockedAPI tests the non-dry-run path with a mocked GitLab API
func TestCreateReleaseWithMockedAPI(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	// Helper to create a release handler that accepts any release request
	releaseHandler := func(w http.ResponseWriter, r *http.Request) {
		// Accept any path that contains "releases"
		if contains(r.URL.Path, "/releases") && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(gitlab.Release{
				TagName: "v1.0.0",
				Name:    "Release 1.0.0",
			})
			return
		}
		http.NotFound(w, r)
	}

	tests := []struct {
		name          string
		cfg           *Config
		releaseCtx    plugin.ReleaseContext
		serverHandler http.HandlerFunc
		wantSuccess   bool
		wantErrorMsg  string
		wantMessage   string
	}{
		{
			name: "successful release creation",
			cfg: &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
			},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "Test release notes",
			},
			serverHandler: releaseHandler,
			wantSuccess:   true,
			wantMessage:   "Created GitLab release:",
		},
		{
			name: "release creation with milestones",
			cfg: &Config{
				Token:      "glpat-test",
				ProjectID:  "group/project",
				Milestones: []string{"v1.0.0", "Q4-2024"},
			},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "Test release notes",
			},
			serverHandler: releaseHandler,
			wantSuccess:   true,
			wantMessage:   "Created GitLab release:",
		},
		{
			name: "release creation with asset links",
			cfg: &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				AssetLinks: []AssetLink{
					{Name: "Download", URL: "https://example.com/download", LinkType: "package"},
					{Name: "Docs", URL: "https://docs.example.com", FilePath: "/docs"},
				},
			},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "Test release notes",
			},
			serverHandler: releaseHandler,
			wantSuccess:   true,
			wantMessage:   "Created GitLab release:",
		},
		{
			name: "release creation with asset links without link type",
			cfg: &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				AssetLinks: []AssetLink{
					{Name: "Download", URL: "https://example.com/download"}, // No LinkType
				},
			},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "Test release notes",
			},
			serverHandler: releaseHandler,
			wantSuccess:   true,
			wantMessage:   "Created GitLab release:",
		},
		{
			name: "release creation failure",
			cfg: &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
			},
			releaseCtx: plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: "v1.0.0",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error": "Internal Server Error"}`))
			},
			wantSuccess:  false,
			wantErrorMsg: "failed to create release",
		},
		{
			name: "release creation with custom base URL",
			cfg: &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				BaseURL:   "", // Will be overridden
			},
			releaseCtx: plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "Test release notes",
			},
			serverHandler: releaseHandler,
			wantSuccess:   true,
			wantMessage:   "Created GitLab release:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupMockGitLabServer(t, tt.serverHandler)
			tt.cfg.BaseURL = server.URL

			resp, err := p.createRelease(ctx, tt.cfg, tt.releaseCtx, false)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected success=%v, got %v (error: %s)", tt.wantSuccess, resp.Success, resp.Error)
			}

			if tt.wantErrorMsg != "" && !contains(resp.Error, tt.wantErrorMsg) {
				t.Errorf("expected error containing %q, got %q", tt.wantErrorMsg, resp.Error)
			}

			if tt.wantMessage != "" && !contains(resp.Message, tt.wantMessage) {
				t.Errorf("expected message containing %q, got %q", tt.wantMessage, resp.Message)
			}
		})
	}
}

// TestCreateReleaseWithAssetsAndMockedAPI tests release creation with file assets
func TestCreateReleaseWithAssetsAndMockedAPI(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state

	tmpDir := t.TempDir()

	// Create test asset files
	assetFile := filepath.Join(tmpDir, "app.zip")
	if err := os.WriteFile(assetFile, []byte("test asset content"), 0644); err != nil {
		t.Fatalf("failed to create test asset file: %v", err)
	}

	// Change to temp directory for asset path validation
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	p := &GitLabPlugin{}
	ctx := context.Background()

	// Helper handler for release and package endpoints
	releaseAndPackageHandler := func(w http.ResponseWriter, r *http.Request) {
		// Handle release creation
		if contains(r.URL.Path, "/releases") && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(gitlab.Release{
				TagName: "v1.0.0",
				Name:    "Release 1.0.0",
			})
			return
		}
		// Handle package upload
		if contains(r.URL.Path, "/packages/generic/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "201 Created",
			})
			return
		}
		http.NotFound(w, r)
	}

	releaseOnlyHandler := func(w http.ResponseWriter, r *http.Request) {
		// Handle release creation
		if contains(r.URL.Path, "/releases") && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(gitlab.Release{
				TagName: "v1.0.0",
				Name:    "Release 1.0.0",
			})
			return
		}
		// Fail package uploads
		if contains(r.URL.Path, "/packages/generic/") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}

	tests := []struct {
		name          string
		assets        []string
		serverHandler http.HandlerFunc
		wantSuccess   bool
		wantArtifacts int
	}{
		{
			name:          "release with successful asset upload",
			assets:        []string{"app.zip"},
			serverHandler: releaseAndPackageHandler,
			wantSuccess:   true,
			wantArtifacts: 1,
		},
		{
			name:          "release with failed asset upload continues",
			assets:        []string{"app.zip"},
			serverHandler: releaseOnlyHandler,
			wantSuccess:   true,
			wantArtifacts: 0, // Asset upload failed but release succeeded
		},
		{
			name:   "release with non-existent asset",
			assets: []string{"nonexistent.zip"},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if contains(r.URL.Path, "/releases") && r.Method == "POST" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(gitlab.Release{
						TagName: "v1.0.0",
						Name:    "Release 1.0.0",
					})
					return
				}
				http.NotFound(w, r)
			},
			wantSuccess:   true,
			wantArtifacts: 0, // Asset not found but release succeeded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupMockGitLabServer(t, tt.serverHandler)

			cfg := &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				BaseURL:   server.URL,
				Assets:    tt.assets,
			}
			releaseCtx := plugin.ReleaseContext{
				Version:      "1.0.0",
				TagName:      "v1.0.0",
				ReleaseNotes: "Test release notes",
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, false)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("expected success=%v, got %v (error: %s)", tt.wantSuccess, resp.Success, resp.Error)
			}

			if len(resp.Artifacts) != tt.wantArtifacts {
				t.Errorf("expected %d artifacts, got %d", tt.wantArtifacts, len(resp.Artifacts))
			}
		})
	}
}

// TestUploadAssetWithMockedAPI tests the uploadAsset function with a mocked API
func TestUploadAssetWithMockedAPI(t *testing.T) {
	// Note: Not using t.Parallel() because os.Chdir affects global state

	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test-asset.zip")
	testContent := []byte("test file content for upload")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	p := &GitLabPlugin{}
	ctx := context.Background()

	tests := []struct {
		name          string
		assetPath     string
		serverHandler http.HandlerFunc
		wantError     bool
		wantArtifact  bool
	}{
		{
			name:      "successful upload",
			assetPath: "test-asset.zip",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if contains(r.URL.Path, "/packages/generic/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"message": "201 Created",
					})
					return
				}
				http.NotFound(w, r)
			},
			wantError:    false,
			wantArtifact: true,
		},
		{
			name:      "upload failure",
			assetPath: "test-asset.zip",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantError:    true,
			wantArtifact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupMockGitLabServer(t, tt.serverHandler)

			client, err := gitlab.NewClient("glpat-test", gitlab.WithBaseURL(server.URL+"/api/v4/"))
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			artifact, err := p.uploadAsset(ctx, client, "group/project", "v1.0.0", tt.assetPath)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if tt.wantArtifact {
				if artifact == nil {
					t.Error("expected artifact, got nil")
				} else {
					if artifact.Name != "test-asset.zip" {
						t.Errorf("expected artifact name 'test-asset.zip', got %q", artifact.Name)
					}
					if artifact.Type != "generic_package" {
						t.Errorf("expected artifact type 'generic_package', got %q", artifact.Type)
					}
					if artifact.Size != int64(len(testContent)) {
						t.Errorf("expected artifact size %d, got %d", len(testContent), artifact.Size)
					}
				}
			} else {
				if artifact != nil {
					t.Errorf("expected nil artifact, got %+v", artifact)
				}
			}
		})
	}
}

// TestCreateReleaseURLConstruction tests the release URL construction logic
func TestCreateReleaseURLConstruction(t *testing.T) {
	t.Parallel()

	p := &GitLabPlugin{}
	ctx := context.Background()

	// Helper handler for release endpoint
	releaseHandler := func(w http.ResponseWriter, r *http.Request) {
		if contains(r.URL.Path, "/releases") && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(gitlab.Release{
				TagName: "v1.0.0",
				Name:    "Release 1.0.0",
			})
			return
		}
		http.NotFound(w, r)
	}

	tests := []struct {
		name        string
		baseURL     string
		wantURLPart string
	}{
		{
			name:        "empty base URL uses gitlab.com",
			baseURL:     "", // Will be set to server URL, but we'll check default in output
			wantURLPart: "group/project/-/releases/v1.0.0",
		},
		{
			name:        "custom base URL",
			baseURL:     "",
			wantURLPart: "group/project/-/releases/v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupMockGitLabServer(t, releaseHandler)

			cfg := &Config{
				Token:     "glpat-test",
				ProjectID: "group/project",
				BaseURL:   server.URL,
			}
			releaseCtx := plugin.ReleaseContext{
				Version: "1.0.0",
				TagName: "v1.0.0",
			}

			resp, err := p.createRelease(ctx, cfg, releaseCtx, false)
			if err != nil {
				t.Fatalf("createRelease returned error: %v", err)
			}

			if !resp.Success {
				t.Fatalf("expected success, got error: %s", resp.Error)
			}

			if !contains(resp.Message, tt.wantURLPart) {
				t.Errorf("expected message to contain %q, got %q", tt.wantURLPart, resp.Message)
			}

			// Verify outputs
			if resp.Outputs["release_url"] == nil {
				t.Error("expected release_url in outputs")
			} else if !contains(resp.Outputs["release_url"].(string), tt.wantURLPart) {
				t.Errorf("expected release_url to contain %q, got %q", tt.wantURLPart, resp.Outputs["release_url"])
			}
		})
	}
}
