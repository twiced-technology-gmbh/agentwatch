package cmd

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/twiced-technology-gmbh/agentwatch/internal/tui"
	"github.com/twiced-technology-gmbh/agentwatch/internal/watcher"
)

func runTUI(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	model := tui.NewBoard(cfg)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startTUIWatcher(ctx, model, p)

	_, err = p.Run()
	return err
}

func startTUIWatcher(ctx context.Context, model *tui.Board, p *tea.Program) {
	paths := model.WatchPaths()
	w, err := watcher.New(paths, func() {
		p.Send(tui.ReloadMsg{})
	})
	if err != nil {
		return // non-fatal: TUI works without live refresh
	}
	defer w.Close()
	w.Run(ctx, nil)
}
