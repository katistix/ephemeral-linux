package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/katistix/ephemeral-linux/internal/config"
	"github.com/katistix/ephemeral-linux/internal/docker"
	"github.com/katistix/ephemeral-linux/internal/tui"
)

func Run() error {
	cfg, configPath, err := config.Load()
	if err != nil {
		return err
	}

	manager := docker.NewManager(cfg)
	if err := manager.CheckDocker(); err != nil {
		return fmt.Errorf("docker is required: %w", err)
	}
	if err := manager.EnsureImage(); err != nil {
		return err
	}
	if err := manager.EnsureContainerRunning(); err != nil {
		return err
	}

	model := tui.NewModel(cfg, configPath, manager)
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err = program.Run()
	return err
}
