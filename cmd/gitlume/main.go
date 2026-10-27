// Command gitlume is a calm, minimal terminal UI for git.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui"
)

func main() {
	os.Exit(run())
}

func run() int {
	repo, err := git.Open(context.Background(), ".")
	if err != nil {
		if errors.Is(err, git.ErrNotRepo) {
			err = errors.New("not a git repository (run gitlume inside a git work tree)")
		}
		fmt.Fprintln(os.Stderr, "gitlume:", err)
		return 1
	}

	// Detect the terminal background before Bubble Tea takes over stdin;
	// otherwise the terminal's reply can be swallowed by the input reader
	// and the first render stalls until the query times out.
	lipgloss.HasDarkBackground()

	p := tea.NewProgram(ui.New(repo), tea.WithAltScreen(), tea.WithReportFocus())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitlume:", err)
		return 1
	}
	return 0
}
