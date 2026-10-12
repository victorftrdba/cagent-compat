package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/aliases"
	"github.com/docker/cagent/pkg/reference"
)

func TestOciRefToFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ociRef   string
		expected string
	}{
		{
			name:     "simple reference",
			ociRef:   "myagent",
			expected: "myagent.yaml",
		},
		{
			name:     "reference with registry and tag",
			ociRef:   "docker.io/myorg/agent:v1",
			expected: "docker.io_myorg_agent_v1.yaml",
		},
		{
			name:     "localhost with port",
			ociRef:   "localhost:5000/test",
			expected: "localhost_5000_test.yaml",
		},
		{
			name:     "reference with digest",
			ociRef:   "myregistry.io/org/app@sha256:abc123",
			expected: "myregistry.io_org_app_sha256_abc123.yaml",
		},
		{
			name:     "already has .yaml extension",
			ociRef:   "myagent.yaml",
			expected: "myagent.yaml",
		},
		{
			name:     "complex path",
			ociRef:   "registry.example.com:443/project/subproject/agent:latest",
			expected: "registry.example.com_443_project_subproject_agent_latest.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := reference.OciRefToFilename(tt.ociRef)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveAgentFile_LocalFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "test-agent.yaml")
	yamlContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Test agent
    instruction: You are a test agent
`
	require.NoError(t, os.WriteFile(yamlFile, []byte(yamlContent), 0o644))

	resolved, err := resolve(yamlFile)
	require.NoError(t, err)

	absPath, err := filepath.Abs(yamlFile)
	require.NoError(t, err)
	assert.Equal(t, absPath, resolved)
}

func TestResolveAgentFile_EmptyIsDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	resolved, err := resolve("")

	require.NoError(t, err)
	assert.Equal(t, "default", resolved)
}

func TestResolveAgentFile_DefaultIsDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	resolved, err := resolve("default")

	require.NoError(t, err)
	assert.Equal(t, "default", resolved)
}

func TestResolveAgentFile_ReplaceAliasWithActualFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Prepare an aliased file: alias -> [xxx]/pirate.yaml
	wd := t.TempDir()
	aliasedAgentFile := filepath.Join(wd, "pirate.yaml")
	require.NoError(t, os.WriteFile(aliasedAgentFile, []byte(`some config`), 0o644))

	all, err := aliases.Load()
	require.NoError(t, err)
	all.Set("other", "another_file.yaml")
	all.Set("alias", aliasedAgentFile)
	require.NoError(t, all.Save())

	resolved, err := resolve("alias")

	require.NoError(t, err)
	assert.Equal(t, aliasedAgentFile, resolved)
}

func TestResolveAgentFile_ReplaceDefaultAliasWithActualFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Prepare an aliased file: alias -> [xxx]/pirate.yaml
	wd := t.TempDir()
	aliasedAgentFile := filepath.Join(wd, "pirate.yaml")
	require.NoError(t, os.WriteFile(aliasedAgentFile, []byte(`some config`), 0o644))

	all, err := aliases.Load()
	require.NoError(t, err)
	all.Set("other", "another_file.yaml")
	all.Set("default", aliasedAgentFile)
	require.NoError(t, all.Save())

	resolved, err := resolve("default")

	require.NoError(t, err)
	assert.Equal(t, aliasedAgentFile, resolved)
}

func TestResolveAgentFile_ReplaceEmptyAliasWithActualFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Prepare an aliased file: alias -> [xxx]/pirate.yaml
	wd := t.TempDir()
	aliasedAgentFile := filepath.Join(wd, "pirate.yaml")
	require.NoError(t, os.WriteFile(aliasedAgentFile, []byte(`some config`), 0o644))

	all, err := aliases.Load()
	require.NoError(t, err)
	all.Set("other", "another_file.yaml")
	all.Set("default", aliasedAgentFile)
	require.NoError(t, all.Save())

	resolved, err := resolve("")

	require.NoError(t, err)
	assert.Equal(t, aliasedAgentFile, resolved)
}

func TestResolveAgentFile_Directory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Should resolve directory to absolute path
	resolved, err := resolve(tmpDir)
	require.NoError(t, err)

	absPath, err := filepath.Abs(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, absPath, resolved)
}

func TestResolveAgentFile_DirectoryWithAgentYaml(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agentContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Test agent in directory
    instruction: You are a test agent
`
	agentFile := filepath.Join(tmpDir, "agent.yaml")
	require.NoError(t, os.WriteFile(agentFile, []byte(agentContent), 0o644))

	// Resolving directory should return directory path
	resolved, err := resolve(tmpDir)
	require.NoError(t, err)

	absPath, err := filepath.Abs(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, absPath, resolved)
}

func TestResolveAgentFile_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "does-not-exist")

	// Should try to treat as OCI reference
	_, err := resolve(nonExistentDir)
	require.NoError(t, err)
}

func TestResolveAgentFile_DirectoryVsFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a directory
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))

	// Create a file with same name but .yaml extension
	yamlFile := filepath.Join(tmpDir, "subdir.yaml")
	yamlContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Test agent file
    instruction: You are a test agent
`
	require.NoError(t, os.WriteFile(yamlFile, []byte(yamlContent), 0o644))

	// Resolve directory
	resolvedDir, err := resolve(subDir)
	require.NoError(t, err)
	absDirPath, err := filepath.Abs(subDir)
	require.NoError(t, err)
	assert.Equal(t, absDirPath, resolvedDir)

	// Resolve file
	resolvedFile, err := resolve(yamlFile)
	require.NoError(t, err)
	absFilePath, err := filepath.Abs(yamlFile)
	require.NoError(t, err)
	assert.Equal(t, absFilePath, resolvedFile)

	// They should be different paths
	assert.NotEqual(t, resolvedDir, resolvedFile)
}

func TestResolveAgentFile_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	emptyDir := filepath.Join(tmpDir, "empty")
	require.NoError(t, os.Mkdir(emptyDir, 0o755))

	// Should still resolve the directory path
	resolved, err := resolve(emptyDir)
	require.NoError(t, err)

	absPath, err := filepath.Abs(emptyDir)
	require.NoError(t, err)
	assert.Equal(t, absPath, resolved)
}

func TestResolveSources(t *testing.T) {
	t.Parallel()

	sources, err := ResolveSources("./testdata/v1.yaml")
	require.NoError(t, err)

	assert.Len(t, sources, 1)
	require.Contains(t, sources, "v1.yaml")
}
