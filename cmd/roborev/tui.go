package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/wesm/roborev/internal/tui"
)

func tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI for monitoring reviews",
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := getDaemonAddr()
			p := tea.NewProgram(tui.NewModel(addr), tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("TUI error: %w", err)
			}
			return nil
		},
	}
}
