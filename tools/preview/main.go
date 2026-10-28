// Command preview renders docs/preview.svg, the animated clip in the README.
// It builds a demo repository in a temp directory, drives the real gitlume
// model through a scripted session and records every frame, so the clip
// always shows what the current code renders.
//
//	go run ./tools/preview            # writes docs/preview.svg
//	go run ./tools/preview -o x.svg
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui"
)

const (
	width  = 120
	height = 26
)

func main() {
	out := flag.String("o", "docs/preview.svg", "output file")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	lipgloss.SetColorProfile(termenv.ANSI)
	lipgloss.SetHasDarkBackground(true)
	// The demo repository must not see the user's git config.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	base, err := os.MkdirTemp("", "gitlume-preview-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(base)
	dir := filepath.Join(base, "aurora")
	if err := setupRepo(dir); err != nil {
		return err
	}

	repo, err := git.Open(context.Background(), dir)
	if err != nil {
		return err
	}
	s := &session{m: ui.New(repo).WithoutPolling()}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	s.drain(s.m.Init())
	script(s)

	if err := os.WriteFile(out, []byte(renderSVG(s.frames, "gitlume: browsing changes, viewing a diff, staging, committing, the log with its graph of all branches and switching branches", width)), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d frames\n", out, len(s.frames))
	return nil
}

// script is the recorded session.
func script(s *session) {
	s.snap(2.6)
	s.key("j", 1.4) // main.go
	s.key("j", 1.6) // forecast.go
	s.key("enter", 2.2)
	s.keyType(tea.KeyCtrlD, 2.4)
	s.key("esc", 1.2)
	s.key(" ", 1.6) // stage forecast.go
	s.key("j", 1.2) // cache.go
	s.key("d", 2.4)
	s.key("n", 1.0)
	s.key("a", 1.6)
	s.key("c", 0.9)
	for _, r := range "Cache hourly forecasts" {
		s.key(string(r), 0.07)
	}
	s.frames[len(s.frames)-1].dur = 1.2
	s.key("enter", 2.6)
	s.key("3", 4.0) // log: every branch's graph, the new commit with its diffstat
	s.key("j", 2.0) // the details follow the cursor
	s.key("2", 2.6) // branches
	s.key("j", 1.4) // fix/timeout
	s.key("j", 1.8) // feature/hourly
	s.key("enter", 3.2)
}

// session drives the model like the Bubble Tea runtime would, but
// synchronously, and records frames.
type session struct {
	m      tea.Model
	frames []frame
}

func (s *session) snap(dur float64) { s.frames = append(s.frames, frame{s.m.View(), dur}) }

func (s *session) send(msg tea.Msg) {
	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	s.drain(cmd)
}

// drain runs cmd and feeds its messages back in, including batches. The
// step limit guards against self-renewing commands.
func (s *session) drain(cmd tea.Cmd) {
	for steps := 0; cmd != nil && steps < 16; steps++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				s.drain(c)
			}
			return
		}
		if msg == nil {
			return
		}
		s.m, cmd = s.m.Update(msg)
	}
}

func (s *session) key(k string, dur float64) {
	switch k {
	case " ":
		s.send(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	case "enter":
		s.send(tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		s.send(tea.KeyMsg{Type: tea.KeyEsc})
	default:
		s.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	s.snap(dur)
}

func (s *session) keyType(t tea.KeyType, dur float64) {
	s.send(tea.KeyMsg{Type: t})
	s.snap(dur)
}

// setupRepo creates the demo project "aurora": a pushed main branch, a few
// feature branches in different states, and uncommitted work in progress.
func setupRepo(dir string) error {
	remote := filepath.Join(filepath.Dir(dir), "remote.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var err error
	// ago sets the commit date of the following commits.
	date := time.Now()
	ago := func(d time.Duration) { date = time.Now().Add(-d) }
	git := func(args ...string) {
		if err != nil {
			return
		}
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		stamp := date.Format(time.RFC3339)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
		if o, e := cmd.CombinedOutput(); e != nil {
			err = fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), e, o)
		}
	}
	put := func(name, content string) {
		if err != nil {
			return
		}
		p := filepath.Join(dir, name)
		if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
			err = os.WriteFile(p, []byte(content), 0o644)
		}
	}
	day := 24 * time.Hour

	git("init", "-q", "--bare", remote)
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Demo")
	git("config", "user.email", "demo@example.com")
	for name, content := range seedFiles {
		put(name, content)
	}
	ago(9 * day)
	git("add", ".")
	git("commit", "-q", "-m", "Initial forecast service")
	git("remote", "add", "origin", remote)
	git("push", "-q", "-u", "origin", "main")

	// A merged and deleted feature branch, for the graph. main stays the
	// oldest local branch, so the branch view's order doesn't change.
	ago(8*day + 6*time.Hour)
	git("switch", "-q", "-c", "feature/alerts")
	git("commit", "-q", "--allow-empty", "-m", "Add storm alerts")
	ago(8 * day)
	git("commit", "-q", "--allow-empty", "-m", "Show alerts in the summary")
	git("switch", "-q", "main")
	ago(8*day + 3*time.Hour)
	git("commit", "-q", "--allow-empty", "-m", "Report wind speed")
	ago(7*day + 20*time.Hour)
	git("merge", "-q", "--no-ff", "-m", "Merge branch 'feature/alerts'", "feature/alerts")
	git("branch", "-q", "-d", "feature/alerts")
	git("push", "-q", "origin", "main")

	ago(4 * day)
	git("switch", "-q", "-c", "feature/hourly")
	git("commit", "-q", "--allow-empty", "-m", "Sketch hourly forecast API")
	ago(2 * day)
	git("commit", "-q", "--allow-empty", "-m", "Render hourly temperatures")
	git("push", "-q", "-u", "origin", "feature/hourly")
	git("switch", "-q", "main")

	ago(6 * day)
	git("switch", "-q", "-c", "old-experiment")
	git("commit", "-q", "--allow-empty", "-m", "Try open-meteo as a backend")
	git("push", "-q", "-u", "origin", "old-experiment")
	git("push", "-q", "origin", "--delete", "old-experiment")
	git("switch", "-q", "main")

	ago(3 * time.Hour)
	git("switch", "-q", "-c", "fix/timeout")
	git("commit", "-q", "--allow-empty", "-m", "Retry failed requests once")
	git("push", "-q", "-u", "origin", "fix/timeout")
	git("reset", "-q", "--hard", "HEAD~1")
	ago(40 * time.Minute)
	git("commit", "-q", "--allow-empty", "-m", "Lower client timeout to 5s")
	git("switch", "-q", "main")

	// Work in progress on main.
	put("README.md", strings.Replace(seedFiles["README.md"], "## Usage", "## Install\n\n    go install example.com/aurora@latest\n\n## Usage", 1))
	git("add", "README.md")
	put("main.go", strings.Replace(seedFiles["main.go"], "\tcity := flag.String(\"city\", \"Berlin\", \"city to forecast\")",
		"\tcity := flag.String(\"city\", \"Berlin\", \"city to forecast\")\n\thours := flag.Int(\"hours\", 12, \"hours to show\")", 1))
	put("internal/forecast/forecast.go", forecastNew)
	put("internal/forecast/cache.go", cacheGo)
	if err == nil {
		err = os.Remove(filepath.Join(dir, "docs/notes.txt"))
	}
	date = time.Now()
	return err
}
