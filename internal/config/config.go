package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ContainerName string `yaml:"container_name"`
	ImageName     string `yaml:"image_name"`
	HostPort      int    `yaml:"host_port"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	WorkspaceDir  string `yaml:"workspace_dir"`
}

func Load() (Config, string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Config{}, "", fmt.Errorf("resolve user config dir: %w", err)
	}

	baseDir := filepath.Join(configDir, "ephemeral-linux")
	configPath := filepath.Join(baseDir, "config.yaml")
	workspaceDir := filepath.Join(baseDir, "workspace")

	cfg := Config{
		ContainerName: "ephemeral-linux",
		ImageName:     "ephemeral-linux:latest",
		HostPort:      2222,
		Username:      "ephemeral",
		Password:      "ephemeral",
		WorkspaceDir:  workspaceDir,
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return Config{}, "", fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return Config{}, "", fmt.Errorf("create workspace dir: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(defaultConfig(cfg)), 0o644); err != nil {
			return Config{}, "", fmt.Errorf("write default config: %w", err)
		}
		return cfg, configPath, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, "", fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, "", fmt.Errorf("parse config: %w", err)
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = workspaceDir
	}
	if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
		return Config{}, "", fmt.Errorf("create configured workspace dir: %w", err)
	}

	return cfg, configPath, nil
}

func defaultConfig(cfg Config) string {
	return fmt.Sprintf(`# ephemeral-linux configuration
#
# This app launches one local Docker container and exposes SSH on the host.
# The defaults are intentionally simple for local-only use.

container_name: %q
image_name: %q
host_port: %d
username: %q
password: %q
workspace_dir: %q
`, cfg.ContainerName, cfg.ImageName, cfg.HostPort, cfg.Username, cfg.Password, cfg.WorkspaceDir)
}
