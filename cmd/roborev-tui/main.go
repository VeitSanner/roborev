package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wesm/roborev/internal/daemon"
	"github.com/wesm/roborev/internal/tui"
)

func main() {
	addr := getDaemonAddr()
	p := tea.NewProgram(tui.NewModel(addr), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func getDaemonAddr() string {
	if info, err := daemon.ReadRuntime(); err == nil {
		return fmt.Sprintf("http://%s", info.Addr)
	}
	return "http://127.0.0.1:7373"
}
