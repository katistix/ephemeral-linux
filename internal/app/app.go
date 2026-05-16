package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/katistix/ephemeral-linux/internal/docker"
	"github.com/katistix/ephemeral-linux/internal/tui"
)

func Run() error {
	manager := docker.NewManager()
	if err := manager.CheckDocker(); err != nil {
		return fmt.Errorf("docker is required: %w", err)
	}

	model := tui.NewModel(manager)
	program := tea.NewProgram(model)
	finalModel, err := program.Run()
	if err != nil {
		return err
	}

	if m, ok := finalModel.(tui.Model); ok {
		return m.Close()
	}
	return nil
}
