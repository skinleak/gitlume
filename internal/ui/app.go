// Package ui contains the Bubble Tea models. Model is the root: it owns
// the child views, routes messages, and runs git commands as tea.Cmds so
// the UI never blocks.
package ui

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/gitlume/internal/git"
	"github.com/skinleak/gitlume/internal/ui/keys"
	"github.com/skinleak/gitlume/internal/ui/theme"
)

// Layout constants, in terminal cells.
const (
	padX         = 2  // left/right page margin
	paneGap      = 4  // space between the file list and the diff
	stackBelow   = 80 // inner widths below this stack the panes vertically
	minListWidth = 24
	maxListWidth = 48
	logListShare = 50 // percent of the width the log's commit list gets
	chromeHeight = 5  // top margin, header, gap, gap, footer

	// maxDiffCacheBytes bounds memory used by cached diffs between refreshes.
	maxDiffCacheBytes = 32 << 20
	// maxCommitCacheEntries bounds the per-branch commit cache.
	maxCommitCacheEntries = 256

	// logPage is how many commits the log loads at a time; the next page
	// loads when the cursor gets within logPrefetch of the end.
	logPage     = 300
	logPrefetch = 50

	// Background polling checks git status every pollMin, or pollCost
	// times as long as the last status took, up to pollMax, so a big repo
	// is polled less often and polling never costs more than ~2% of a core.
	pollMin  = 2 * time.Second
	pollMax  = time.Minute
	pollCost = 50

	// infoTimeout is how long an info message stays in the footer before
	// the key hints come back. Errors stay until the next key: they can
	// quote a hook's output, which takes longer to read.
	infoTimeout = 3 * time.Second
)

// viewKind is the tab selected with the number keys.
type viewKind int

const (
	viewFiles viewKind = iota
	viewLog
	viewCount // the number of views; ← and → wrap around
)

type focusArea int

const (
	focusFiles focusArea = iota
	focusDiff
)

// Model is the root Bubble Tea model.
type Model struct {
	repo        *git.Repo
	keys        keys.KeyMap
	confirmKeys keys.ConfirmKeyMap
	styles      theme.Styles
	help        help.Model

	files     fileList
	diff      diffView
	branches  branchList
	commits   commitsView
	log       logList
	commit    prompt // the commit message
	newBranch prompt // the name of a branch to create
	confirm   confirmPrompt

	status    git.Status // the last one applied, for polls to compare with
	statusSeq int
	loading   bool      // a status load is running; only one runs at a time
	reload    bool      // a refresh was asked for while loading
	statusAt  time.Time // when the last status finished loading
	pollEvery time.Duration
	polling   bool

	diffSeq   int
	diffReq   request
	diffCache map[diffKey]diffContent
	cacheSize int // bytes held in diffCache

	branchSeq   int
	commitSeq   int
	commitReq   request
	commitCache map[string][]git.Commit // by full ref

	view          viewKind
	branchesOpen  bool   // the branch list covers the view until checkout or esc
	newBranchFrom string // the full ref a new branch starts at; empty is HEAD
	focus         focusArea
	showHelp      bool
	helpOffset    int    // first line of the keybindings shown when they scroll
	zoomed        bool   // the diff fills the body
	busy          bool   // a mutating git command is running
	pending       string // what it is doing, shown in the footer until it ends
	message       string
	isErr         bool
	messageSeq    int           // counts messages, so a fade only clears its own
	infoTimeout   time.Duration // 0 keeps info messages until the next key

	width, height int
}

// New returns the root model for repo.
func New(repo *git.Repo) Model {
	st := theme.Default()
	return Model{
		repo:        repo,
		keys:        keys.Default(),
		confirmKeys: keys.DefaultConfirm(),
		styles:      st,
		help:        newHelp(st),
		commit:      newPrompt(st, "commit › ", "Commit message", keys.DefaultPrompt()),
		newBranch:   newPrompt(st, "new branch › ", "branch name", keys.DefaultBranchPrompt()),
		diffCache:   make(map[diffKey]diffContent),
		commitCache: make(map[string][]git.Commit),
		log:         logList{limit: logPage},
		loading:     true, // by Init
		pollEvery:   pollMin,
		polling:     true,
		infoTimeout: infoTimeout,
	}
}

// WithoutPolling turns background status checks and fading messages off,
// for tools and tests that drive the model synchronously: their ticks
// would make them sleep. Messages then stay until the next key.
func (m Model) WithoutPolling() Model {
	m.polling, m.infoTimeout = false, 0
	return m
}

// Init loads the initial status and starts polling. Init cannot mutate
// the model, so New marks the load as running and Init uses the current
// sequence number instead of calling refresh.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadStatus(m.repo, m.statusSeq, false, m.status), m.nextPoll())
}

// Update routes messages to handlers. An info message shown by any of
// them fades after infoTimeout.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	seq := m.messageSeq
	m, cmd := m.update(msg)
	if m.messageSeq != seq && !m.isErr && m.infoTimeout > 0 {
		cmd = tea.Batch(cmd, fadeMessage(m.infoTimeout, m.messageSeq))
	}
	return m, cmd
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.FocusMsg:
		return m, m.refresh()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case statusLoadedMsg:
		return m, m.applyStatus(msg)
	case pollTickMsg:
		return m, m.poll(msg.at)
	case diffTickMsg:
		return m, m.fetchDiff(msg.seq)
	case diffLoadedMsg:
		m.applyDiff(msg)
		return m, nil
	case branchesLoadedMsg:
		return m, m.applyBranches(msg)
	case commitsTickMsg:
		return m, m.fetchCommits(msg.seq)
	case commitsLoadedMsg:
		m.applyCommits(msg)
		return m, nil
	case logLoadedMsg:
		return m, m.applyLog(msg)
	case fadeMsg:
		if msg.seq == m.messageSeq && !m.isErr {
			m.message = ""
		}
		return m, nil
	case opDoneMsg:
		m.busy, m.pending = false, ""
		m.setMessage(msg.info, msg.err)
		return m, m.refresh()
	}
	if p := m.activePrompt(); p != nil {
		return m, p.update(msg)
	}
	return m, nil
}

// activePrompt is the open prompt, if any.
func (m *Model) activePrompt() *prompt {
	switch {
	case m.commit.active:
		return &m.commit
	case m.newBranch.active:
		return &m.newBranch
	}
	return nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.commit.active {
		return m, m.handleCommitKey(msg)
	}
	if m.newBranch.active {
		return m, m.handleNewBranchKey(msg)
	}
	if m.confirm.active {
		return m, m.handleConfirmKey(msg)
	}
	m.message, m.isErr = "", false

	if m.showHelp {
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help, m.keys.Back):
			m.showHelp = false
		case key.Matches(msg, m.keys.Down):
			m.scrollHelp(1)
		case key.Matches(msg, m.keys.Up):
			m.scrollHelp(-1)
		case key.Matches(msg, m.keys.ScrollDown):
			m.scrollHelp(m.bodyHeight() / 2)
		case key.Matches(msg, m.keys.ScrollUp):
			m.scrollHelp(-m.bodyHeight() / 2)
		}
		return m, nil
	}

	if m.zoomed {
		return m, m.handleZoomedKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Files):
		return m, m.showView(viewFiles)
	case key.Matches(msg, m.keys.Log):
		return m, m.showView(viewLog)
	case key.Matches(msg, m.keys.NextView):
		return m, m.showView((m.view + 1) % viewCount)
	case key.Matches(msg, m.keys.PrevView):
		return m, m.showView((m.view + viewCount - 1) % viewCount)
	case key.Matches(msg, m.keys.Branches):
		if m.branchesOpen {
			return m, m.showView(m.view)
		}
		return m, m.openBranches()
	}
	if m.branchesOpen {
		return m, m.handleBranchKey(msg)
	}
	if m.view == viewLog {
		return m, m.handleLogKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.openHelp()
	case key.Matches(msg, m.keys.Refresh):
		return m, m.refresh()
	case key.Matches(msg, m.keys.Focus):
		m.toggleFocus()
	case key.Matches(msg, m.keys.ScrollDown):
		m.diff.scroll(m.diff.height / 2)
	case key.Matches(msg, m.keys.ScrollUp):
		m.diff.scroll(-m.diff.height / 2)
	case key.Matches(msg, m.keys.Down):
		return m, m.moveOrScroll(1)
	case key.Matches(msg, m.keys.Up):
		return m, m.moveOrScroll(-1)
	case key.Matches(msg, m.keys.Toggle):
		return m, m.toggleSelected()
	case key.Matches(msg, m.keys.StageAll):
		return m, m.stageAll()
	case key.Matches(msg, m.keys.Discard):
		m.askDiscard()
	case key.Matches(msg, m.keys.DiscardAll):
		m.askDiscardAll()
	case key.Matches(msg, m.keys.Open):
		m.setZoom(true)
	case key.Matches(msg, m.keys.Commit):
		return m, m.openCommit()
	case key.Matches(msg, m.keys.Pull):
		return m, m.pull()
	case key.Matches(msg, m.keys.Push):
		return m, m.push()
	}
	return m, nil
}

// handleLogKey handles keys in the log view. Like the files view, j/k move
// through the graph's commits or scroll the details, depending on focus.
func (m *Model) handleLogKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.openHelp()
	case key.Matches(msg, m.keys.Refresh):
		return m.refresh()
	case key.Matches(msg, m.keys.Focus):
		m.toggleFocus()
	case key.Matches(msg, m.keys.ScrollDown):
		m.diff.scroll(m.diff.height / 2)
	case key.Matches(msg, m.keys.ScrollUp):
		m.diff.scroll(-m.diff.height / 2)
	case key.Matches(msg, m.keys.Down):
		return m.moveOrScroll(1)
	case key.Matches(msg, m.keys.Up):
		return m.moveOrScroll(-1)
	case key.Matches(msg, m.keys.Open):
		m.setZoom(true)
	case key.Matches(msg, m.keys.Pull):
		return m.pull()
	case key.Matches(msg, m.keys.Push):
		return m.push()
	}
	return nil
}

// handleBranchKey handles keys while the branch list is open. esc returns
// to the view it was opened from.
func (m *Model) handleBranchKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Back):
		return m.showView(m.view)
	case key.Matches(msg, m.keys.Help):
		m.openHelp()
	case key.Matches(msg, m.keys.Refresh):
		return m.refresh()
	case key.Matches(msg, m.keys.Down):
		if m.branches.move(1) {
			return m.requestCommits()
		}
	case key.Matches(msg, m.keys.Up):
		if m.branches.move(-1) {
			return m.requestCommits()
		}
	case key.Matches(msg, m.keys.ScrollDown):
		m.commits.scroll(m.commits.height / 2)
	case key.Matches(msg, m.keys.ScrollUp):
		m.commits.scroll(-m.commits.height / 2)
	case key.Matches(msg, m.keys.Checkout):
		return m.checkoutSelected()
	case key.Matches(msg, m.keys.NewBranch):
		return m.openNewBranch()
	}
	return nil
}

// showView switches tabs, closing the branch list. The log is loaded every
// time it is shown, so it is never stale; the files view catches up on its
// diff, which may have changed while the branch list covered it.
func (m *Model) showView(v viewKind) tea.Cmd {
	if v == m.view && !m.branchesOpen {
		return nil
	}
	m.view, m.focus, m.branchesOpen = v, focusFiles, false
	m.diffSeq++ // a diff still loading belongs to the previous view
	m.diffReq.stop()
	if v == viewLog {
		if !m.diff.key.isCommit() {
			m.diff.clear() // a file diff must not show in the log
		}
		return m.reloadLog()
	}
	return m.requestDiff()
}

// openBranches shows the branch list over the current view. It is loaded
// every time it opens, so it is never stale.
func (m *Model) openBranches() tea.Cmd {
	m.branchesOpen = true
	m.diffSeq++
	m.diffReq.stop()
	return m.reloadBranches()
}

func (m *Model) checkoutSelected() tea.Cmd {
	b, ok := m.branches.selected()
	if !ok {
		return nil
	}
	// A remote branch that already has a local counterpart switches to it.
	if b.Remote {
		if local, ok := m.branches.localFor(b); ok {
			b = local
		}
	}
	name := sanitize(b.Name)
	if b.Current {
		m.setMessage("Already on "+name, nil)
		return nil
	}
	if b.Remote {
		_, short, _ := strings.Cut(name, "/")
		name = short
	}
	repo := m.repo
	cmd := m.startOp("Switching to "+name+"…", "Switched to "+name, func(ctx context.Context) error {
		return repo.Checkout(ctx, b)
	})
	if cmd != nil {
		// Back to the view underneath; the refresh after the switch
		// reloads it.
		m.branchesOpen = false
	}
	return cmd
}

func (m *Model) handleCommitKey(msg tea.KeyMsg) tea.Cmd {
	m.message, m.isErr = "", false
	switch {
	case key.Matches(msg, m.commit.keys.Cancel):
		m.commit.close()
		return nil
	case key.Matches(msg, m.commit.keys.Confirm):
		text := strings.TrimSpace(m.commit.input.Value())
		if text == "" {
			m.setMessage("", git.ErrEmptyMessage)
			return nil
		}
		if m.busy {
			// Keep the prompt and draft; the user can press enter again.
			m.setMessage("Still working, try again in a moment", nil)
			return nil
		}
		m.commit.close()
		m.commit.input.Reset()
		repo := m.repo
		return m.startOp("Committing…", "Committed", func(ctx context.Context) error {
			return repo.Commit(ctx, text)
		})
	}
	return m.commit.update(msg)
}

// openNewBranch asks for the name of a branch to create from the selected
// one; the list opens on the current branch, so that is HEAD by default.
func (m *Model) openNewBranch() tea.Cmd {
	m.newBranchFrom, m.newBranch.label = "", "new branch › "
	if b, ok := m.branches.selected(); ok && !b.Current {
		m.newBranchFrom = b.Ref()
		m.newBranch.label = "new branch from " + sanitize(b.Name) + " › "
	}
	return m.newBranch.open(m.promptInputWidth(&m.newBranch))
}

// handleNewBranchKey creates the branch on enter and switches to it, back
// in the view beneath the list. Spaces become dashes, which git would
// refuse in a name; other invalid names get git's own explanation.
func (m *Model) handleNewBranchKey(msg tea.KeyMsg) tea.Cmd {
	m.message, m.isErr = "", false
	switch {
	case key.Matches(msg, m.newBranch.keys.Cancel):
		m.newBranch.close()
		return nil
	case key.Matches(msg, m.newBranch.keys.Confirm):
		name := strings.Join(strings.Fields(m.newBranch.input.Value()), "-")
		if name == "" {
			m.setMessage("", git.ErrEmptyBranchName)
			return nil
		}
		if m.busy {
			m.setMessage("Still working, try again in a moment", nil)
			return nil
		}
		m.newBranch.close()
		m.newBranch.input.Reset()
		m.branchesOpen = false
		repo, from := m.repo, m.newBranchFrom
		return m.startOp("", "Switched to new branch "+sanitize(name), func(ctx context.Context) error {
			return repo.CreateBranch(ctx, name, from)
		})
	}
	return m.newBranch.update(msg)
}

// handleZoomedKey handles keys while the diff is fullscreen. Only viewing
// keys work here, so nothing changes the repository by surprise.
func (m *Model) handleZoomedKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Back, m.keys.Open):
		m.setZoom(false)
	case key.Matches(msg, m.keys.Help):
		m.openHelp()
	case key.Matches(msg, m.keys.Refresh):
		return m.refresh()
	case key.Matches(msg, m.keys.Down):
		m.diff.scroll(1)
	case key.Matches(msg, m.keys.Up):
		m.diff.scroll(-1)
	case key.Matches(msg, m.keys.ScrollDown):
		m.diff.scroll(m.diff.height / 2)
	case key.Matches(msg, m.keys.ScrollUp):
		m.diff.scroll(-m.diff.height / 2)
	}
	return nil
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	m.message, m.isErr = "", false
	switch {
	case key.Matches(msg, m.confirmKeys.No):
		m.confirm.close()
	case key.Matches(msg, m.confirmKeys.Yes):
		if m.busy {
			// Keep the question; the user can press y again.
			m.setMessage("Still working, try again in a moment", nil)
			return nil
		}
		done, op := m.confirm.done, m.confirm.op
		m.confirm.close()
		return m.startOp("", done, op)
	}
	return nil
}

// setZoom shows the selected diff or commit fullscreen or returns to the
// panes.
func (m *Model) setZoom(on bool) {
	if _, ok := m.diffTarget(); on && !ok {
		return
	}
	m.zoomed = on
	m.resize()
}

func (m *Model) toggleFocus() {
	if m.focus == focusFiles {
		m.focus = focusDiff
	} else {
		m.focus = focusFiles
	}
}

func (m *Model) moveOrScroll(delta int) tea.Cmd {
	if m.focus == focusDiff {
		m.diff.scroll(delta)
		return nil
	}
	if h := m.history(); h != nil {
		moved := h.move(delta)
		var more tea.Cmd
		if h.more && h.cursor >= len(h.items)-logPrefetch {
			h.more = false // one page request at a time
			h.limit += logPage
			more = m.reloadLog()
		}
		if !moved {
			return more
		}
		return tea.Batch(m.requestDiff(), more)
	}
	if !m.files.move(delta) {
		return nil
	}
	return m.requestDiff()
}

func (m *Model) toggleSelected() tea.Cmd {
	c, ok := m.files.selected()
	if !ok {
		return nil
	}
	// Closures run on another goroutine: capture values, never the model.
	repo := m.repo
	if c.Group == git.Staged {
		paths := []string{c.Path}
		if c.OrigPath != "" {
			paths = append(paths, c.OrigPath)
		}
		return m.startOp("", "", func(ctx context.Context) error {
			return repo.Unstage(ctx, paths...)
		})
	}
	return m.startOp("", "", func(ctx context.Context) error {
		return repo.Stage(ctx, c.Path)
	})
}

func (m *Model) stageAll() tea.Cmd {
	if m.files.count(git.Unstaged)+m.files.count(git.Untracked) == 0 {
		m.setMessage("Nothing to stage", nil)
		return nil
	}
	return m.startOp("", "", m.repo.StageAll)
}

func (m *Model) askDiscard() {
	c, ok := m.files.selected()
	if !ok {
		return
	}
	if c.Group == git.Untracked && strings.HasSuffix(c.Path, "/") {
		m.setMessage("", git.ErrNestedRepo)
		return
	}
	question := "Discard all changes to " + displayPath(c) + "? This cannot be undone."
	if c.Group == git.Untracked {
		question = "Delete untracked " + sanitize(c.Path) + "? This cannot be undone."
	}
	// A staged rename must be restored together with its source path, even
	// when the unstaged row of the same file is selected.
	var extra []string
	if orig := m.files.renameSource(c.Path); orig != "" && c.Group != git.Untracked {
		extra = []string{orig}
	}
	repo := m.repo
	m.confirm.ask(question, "Discarded "+sanitize(c.Path), func(ctx context.Context) error {
		return repo.Discard(ctx, c, extra...)
	})
}

func (m *Model) askDiscardAll() {
	if len(m.files.items) == 0 {
		m.setMessage("Nothing to discard", nil)
		return
	}
	m.confirm.ask("Discard ALL uncommitted changes and delete untracked files? This cannot be undone.",
		"Discarded all changes", m.repo.DiscardAll)
}

func (m *Model) openCommit() tea.Cmd {
	if m.files.count(git.Staged) == 0 {
		m.setMessage("Nothing staged to commit", nil)
		return nil
	}
	return m.commit.open(m.promptInputWidth(&m.commit))
}

// pull fast-forwards the current branch from its upstream.
func (m *Model) pull() tea.Cmd {
	return m.start("Pulling…", runSync("Pulled", "Already up to date", m.repo.Pull))
}

// push sends the current branch to its upstream, setting one on the first
// push.
// Pushing right after editing, before committing, is a common slip, so the
// message says why there was nothing to push.
func (m *Model) push() tea.Cmd {
	same := "Nothing to push"
	if len(m.files.items) > 0 {
		same = "Nothing to push: your changes aren't committed yet"
	}
	return m.start("Pushing…", runSync("Pushed", same, m.repo.Push))
}

// startOp runs a mutating git command that reports done when it succeeds.
func (m *Model) startOp(pending, done string, op func(context.Context) error) tea.Cmd {
	return m.start(pending, runOp(done, op))
}

// start runs cmd, a mutating git command that ends with an opDoneMsg. Only
// one runs at a time so commands never contend for the index lock; keys
// pressed meanwhile are ignored. pending stays in the footer until it ends,
// so a slow push is never mistaken for a finished one.
func (m *Model) start(pending string, cmd tea.Cmd) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy, m.pending = true, pending
	return cmd
}

// refresh reloads the status and everything shown from it. Only one
// status load runs at a time: a refresh asked for meanwhile runs when it
// finishes, so bursts of focus events or operations cost one extra load.
func (m *Model) refresh() tea.Cmd {
	if m.loading {
		m.reload = true
		return nil
	}
	return m.loadStatus(false)
}

func (m *Model) loadStatus(poll bool) tea.Cmd {
	m.statusSeq++
	m.loading = true
	return loadStatus(m.repo, m.statusSeq, poll, m.status)
}

// nextPoll schedules the next background check, or nil when polling is off.
func (m Model) nextPoll() tea.Cmd {
	if !m.polling {
		return nil
	}
	return pollTick(m.pollEvery)
}

// poll checks the status in the background, which catches changes made
// while the window has focus or in terminals without focus reporting. It
// skips a tick while anything else is going on, or right after a refresh.
func (m *Model) poll(now time.Time) tea.Cmd {
	next := m.nextPoll()
	if m.loading || m.busy || m.activePrompt() != nil || m.confirm.active || now.Sub(m.statusAt) < m.pollEvery/2 {
		return next
	}
	return tea.Batch(next, m.loadStatus(true))
}

func (m *Model) applyStatus(msg statusLoadedMsg) tea.Cmd {
	if msg.seq != m.statusSeq {
		return nil
	}
	m.loading = false
	m.statusAt = msg.at
	m.pollEvery = min(max(pollCost*msg.took, pollMin), pollMax)
	cmd := m.showStatus(msg)
	if m.reload {
		// Compared with the status just shown, so it starts after it.
		m.reload = false
		return tea.Batch(cmd, m.loadStatus(false))
	}
	return cmd
}

// showStatus applies a loaded status. A poll that found nothing new
// reloads only the diff on screen: a file that was already modified can
// change without changing the status.
func (m *Model) showStatus(msg statusLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.setMessage("", msg.err)
		return nil
	}
	if msg.unchanged {
		if m.view != viewFiles || m.branchesOpen {
			return nil
		}
		m.clearDiffCache()
		return m.requestDiff()
	}
	m.status = msg.status
	m.files.setChanges(allChanges(msg.status))
	m.clearDiffCache()
	if _, ok := m.diffTarget(); !ok && m.zoomed {
		m.setZoom(false)
	}
	switch {
	case m.branchesOpen:
		// The view underneath is reloaded when the list closes.
		return m.reloadBranches()
	case m.view == viewLog:
		return m.reloadLog()
	}
	return m.requestDiff()
}

// history is the log's commit list, or nil in other views.
func (m *Model) history() *logList {
	if m.view == viewLog {
		return &m.log
	}
	return nil
}

// reloadLog loads the first limit commits of all branches. An unborn
// branch has no HEAD, and git would fail on it; the other branches still
// show.
func (m *Model) reloadLog() tea.Cmd {
	l := &m.log
	l.seq++
	return loadLog(l.req.start(), m.repo, l.seq, l.limit, !m.status.Branch.Unborn())
}

func (m *Model) applyLog(msg logLoadedMsg) tea.Cmd {
	l := &m.log
	if msg.seq != l.seq {
		return nil
	}
	l.req.stop()
	if msg.err != nil {
		m.setMessage("", msg.err)
		return nil
	}
	l.set(msg.commits, msg.lanes, len(msg.commits) == msg.limit, m.status.Branch.Head)
	if m.view != viewLog || m.branchesOpen {
		return nil
	}
	return m.requestDiff()
}

func (m *Model) reloadBranches() tea.Cmd {
	m.branchSeq++
	return loadBranches(m.repo, m.branchSeq)
}

func (m *Model) applyBranches(msg branchesLoadedMsg) tea.Cmd {
	if msg.seq != m.branchSeq {
		return nil
	}
	if msg.err != nil {
		m.setMessage("", msg.err)
		return nil
	}
	m.branches.set(msg.branches, msg.now)
	clear(m.commitCache)
	return m.requestCommits()
}

// requestCommits shows the selected branch's commits, debounced and cached
// like diffs.
func (m *Model) requestCommits() tea.Cmd {
	m.commitSeq++
	m.commitReq.stop()
	b, ok := m.branches.selected()
	if !ok {
		m.commits.clear()
		return nil
	}
	if cached, ok := m.commitCache[b.Ref()]; ok {
		m.commits.show(b.Ref(), cached, m.branches.now)
		return nil
	}
	return commitsTick(m.commitSeq)
}

func (m *Model) fetchCommits(seq int) tea.Cmd {
	b, ok := m.branches.selected()
	if seq != m.commitSeq || !ok {
		return nil
	}
	return loadCommits(m.commitReq.start(), m.repo, seq, b.Ref())
}

func (m *Model) applyCommits(msg commitsLoadedMsg) {
	if msg.seq != m.commitSeq {
		return
	}
	m.commitReq.stop()
	if msg.err != nil {
		m.setMessage("", msg.err)
		return
	}
	if len(m.commitCache) >= maxCommitCacheEntries {
		clear(m.commitCache)
	}
	m.commitCache[msg.ref] = msg.commits
	m.commits.show(msg.ref, msg.commits, m.branches.now)
}

// requestDiff shows the selected change's diff, from cache when possible.
// Otherwise it schedules a debounced load; the current diff stays visible
// until the new one arrives, so the panel never flickers. A diff of the
// other kind (a file in the log, a commit in the files view) is cleared
// instead: it would be wrong, not just old.
func (m *Model) requestDiff() tea.Cmd {
	m.diffSeq++
	m.diffReq.stop()
	k, ok := m.diffTarget()
	if !ok {
		m.diff.clear()
		return nil
	}
	if cached, ok := m.diffCache[k]; ok {
		m.diff.show(k, cached)
		return nil
	}
	if m.diff.key.isCommit() != k.isCommit() {
		m.diff.clear()
	}
	return diffTick(m.diffSeq)
}

// diffTarget is what the diff pane shows in the current view: the selected
// change, or the selected commit of the log.
func (m Model) diffTarget() (diffKey, bool) {
	switch m.view {
	case viewFiles:
		c, ok := m.files.selected()
		return keyOf(c), ok
	case viewLog:
		c, ok := m.log.selected()
		return diffKey{commit: c.Hash}, ok
	}
	return diffKey{}, false
}

func (m *Model) fetchDiff(seq int) tea.Cmd {
	k, ok := m.diffTarget()
	if seq != m.diffSeq || !ok {
		return nil
	}
	if k.isCommit() {
		return loadShow(m.diffReq.start(), m.repo, seq, k.commit, m.layout().diffW)
	}
	c, _ := m.files.selected()
	return loadDiff(m.diffReq.start(), m.repo, seq, c)
}

func (m *Model) applyDiff(msg diffLoadedMsg) {
	if msg.seq != m.diffSeq {
		return
	}
	m.diffReq.stop()
	if msg.err != nil {
		m.setMessage("", msg.err)
		return
	}
	if m.cacheSize+msg.content.size > maxDiffCacheBytes {
		m.clearDiffCache()
	}
	m.diffCache[msg.key] = msg.content
	m.cacheSize += msg.content.size
	m.diff.show(msg.key, msg.content)
}

func (m *Model) clearDiffCache() {
	clear(m.diffCache)
	m.cacheSize = 0
}

// setMessage shows info or err in the footer. Errors can quote hook
// output, which is often colored, so they are sanitized like file names.
func (m *Model) setMessage(info string, err error) {
	if err != nil {
		m.message, m.isErr = sanitize(err.Error()), true
		m.messageSeq++
		return
	}
	if info != "" {
		m.message, m.isErr = sanitize(info), false
		m.messageSeq++
	}
}

// layout describes the pane sizes for the current window.
type layout struct {
	listW, listH int
	diffW, diffH int
	stacked      bool
}

func (m Model) innerWidth() int { return max(1, m.width-2*padX) }

func (m Model) bodyHeight() int { return max(2, m.height-chromeHeight) }

// layout is the current view's layout: the diff alone when zoomed, else a
// list and a pane.
func (m Model) layout() layout {
	if m.zoomed {
		return layout{diffW: m.innerWidth(), diffH: m.bodyHeight()}
	}
	return m.paneLayout()
}

// paneLayout puts the list left of the pane, or above it when narrow. The
// log's rows carry lanes, a hash and refs, so its list gets more width.
func (m Model) paneLayout() layout {
	w, bodyH := m.innerWidth(), m.bodyHeight()
	if w < stackBelow {
		listH := max(1, bodyH*2/5)
		return layout{listW: w, listH: listH, diffW: w, diffH: max(1, bodyH-listH-1), stacked: true}
	}
	listW := clamp(w*35/100, minListWidth, maxListWidth)
	if m.view == viewLog && !m.branchesOpen {
		listW = (w - paneGap) * logListShare / 100
	}
	return layout{listW: listW, listH: bodyH, diffW: w - listW - paneGap, diffH: bodyH}
}

// resize sizes every view, not only the one shown: switching views does
// not resize. Only the list widths differ between views, not the heights.
func (m *Model) resize() {
	if m.zoomed {
		m.diff.setHeight(m.bodyHeight())
	} else {
		// The lists keep their scroll position while hidden.
		l := m.paneLayout()
		m.files.setHeight(l.listH)
		m.branches.setHeight(l.listH)
		m.log.setHeight(l.listH)
		m.commits.setHeight(l.diffH)
		m.diff.setHeight(l.diffH)
	}
	m.help.Width = m.innerWidth()
	m.commit.input.Width = m.promptInputWidth(&m.commit)
	m.newBranch.input.Width = m.promptInputWidth(&m.newBranch)
}

// promptInputWidth leaves room on the footer line for p's label and its
// key hints. textinput renders one cell more than its Width for the
// cursor, hence the extra 1.
func (m Model) promptInputWidth(p *prompt) int {
	hint := ansi.StringWidth(m.help.ShortHelpView(p.keys.ShortHelp()))
	return max(1, m.innerWidth()-ansi.StringWidth(p.label)-hint-paneGap-1)
}

// View renders the whole screen. It is assembled line by line from parts
// that are already cut to their width: Lip Gloss's Width, Height and Join
// measure every cell of the frame again and doubled the cost of a frame.
// Lines are not padded on the right; the renderer clears the rest of each.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyH := m.bodyHeight()
	var body []string
	if m.showHelp {
		body = m.helpOverlay(m.innerWidth(), bodyH)
	} else {
		body = m.bodyLines(m.layout())
	}
	page := make([]string, 0, bodyH+chromeHeight)
	page = append(page, "", m.headerView(), "")
	for i := range bodyH {
		if i < len(body) {
			page = append(page, body[i])
		} else {
			page = append(page, "")
		}
	}
	page = append(page, "", m.footerView())
	page = page[:min(len(page), m.height)] // windows too small for the chrome

	margin := strings.Repeat(" ", padX)
	tiny := padX+m.innerWidth() > m.width
	var b strings.Builder
	for i, l := range page {
		if i > 0 {
			b.WriteByte('\n')
		}
		if l == "" {
			continue
		}
		l = margin + l
		if tiny {
			l = ansi.Truncate(l, m.width, "")
		}
		b.WriteString(l)
	}
	return b.String()
}

// bodyLines renders the body: the list and its pane side by side, or
// stacked when narrow. Pane lines are already cut to the pane width.
func (m Model) bodyLines(l layout) []string {
	if m.zoomed {
		return lines(m.diff.view(m.styles, l.diffW))
	}
	var list, pane string
	switch {
	case m.branchesOpen:
		list = m.branches.view(m.styles, l.listW)
		pane = m.commits.view(m.styles, l.diffW)
	case m.view == viewLog:
		list = m.log.view(m.styles, l.listW, m.focus == focusFiles)
		pane = m.diff.view(m.styles, l.diffW)
	default:
		list = m.files.view(m.styles, l.listW, m.focus == focusFiles)
		pane = m.diff.view(m.styles, l.diffW)
	}
	left, right := lines(list), lines(pane)
	if l.stacked {
		out := make([]string, l.listH, l.listH+1+len(right))
		for i := range min(len(left), l.listH) {
			out[i] = fit(left[i], l.listW, false)
		}
		return append(append(out, ""), right...)
	}
	gap := strings.Repeat(" ", paneGap)
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		var a string
		if i < len(left) {
			a = left[i]
		}
		if i >= len(right) || right[i] == "" {
			out[i] = fit(a, l.listW, false)
			continue
		}
		out[i] = fit(a, l.listW, true) + gap + right[i]
	}
	return out
}

// lines splits rendered text into lines; empty text has none.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// fit cuts s to width cells and, when pad is set, fills it up to width.
func fit(s string, width int, pad bool) string {
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	if pad && w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func (m Model) headerView() string {
	st := m.styles
	repo := st.Muted.Render(sanitize(filepath.Base(m.repo.Root())))
	var branch string
	switch b := m.status.Branch; {
	case b.Detached() && len(b.OID) >= 7:
		branch = st.Branch.Render("detached") + " " + st.Muted.Render(b.OID[:7])
	case b.Detached():
		branch = st.Branch.Render("detached")
	default:
		branch = st.Branch.Render(sanitize(b.Head))
	}
	parts := []string{repo, branch}
	if m.status.Branch.Unborn() && !m.status.Branch.Detached() {
		parts = append(parts, st.Subtle.Render("no commits yet"))
	}
	if m.status.Branch.Ahead > 0 {
		parts = append(parts, st.Ahead.Render("↑"+strconv.Itoa(m.status.Branch.Ahead)))
	}
	if m.status.Branch.Behind > 0 {
		parts = append(parts, st.Behind.Render("↓"+strconv.Itoa(m.status.Branch.Behind)))
	}
	if m.zoomed {
		if c, ok := m.files.selected(); ok && m.view == viewFiles {
			parts = append(parts, st.Text.Render(displayPath(c)))
		}
		if h := m.history(); h != nil {
			c, ok := h.selected()
			if ok {
				parts = append(parts, st.CommitHash.Render(sanitize(c.Hash))+" "+st.Text.Render(sanitize(c.Subject)))
			}
		}
	}
	left := strings.Join(parts, "  ")
	// The tabs are right-aligned and dropped when they don't fit.
	tabs := m.tabsView()
	if gap := m.innerWidth() - ansi.StringWidth(left) - ansi.StringWidth(tabs); gap >= paneGap {
		return left + strings.Repeat(" ", gap) + tabs
	}
	return ansi.Truncate(left, m.innerWidth(), "…")
}

func (m Model) tabsView() string {
	st := m.styles
	tab := func(b key.Binding, label string, v viewKind) string {
		style := st.Tab
		if m.view == v {
			style = st.TabActive
		}
		return st.TabKey.Render(b.Help().Key) + " " + style.Render(label)
	}
	return tab(m.keys.Files, "Files", viewFiles) + "   " + tab(m.keys.Log, "Log", viewLog)
}

func (m Model) footerView() string {
	return ansi.Truncate(m.footerLine(), m.innerWidth(), "…")
}

func (m Model) footerLine() string {
	if p := m.activePrompt(); p != nil {
		prompt := p.view(m.styles)
		hint := m.help.ShortHelpView(p.keys.ShortHelp())
		if m.message != "" {
			hint = m.messageView()
		}
		gap := max(paneGap, m.innerWidth()-ansi.StringWidth(prompt)-ansi.StringWidth(hint))
		return prompt + strings.Repeat(" ", gap) + hint
	}
	if m.confirm.active {
		hint := m.help.ShortHelpView(m.confirmKeys.ShortHelp())
		if m.message != "" {
			hint = m.messageView()
		}
		// Shorten the question, never the y/n hint.
		room := max(1, m.innerWidth()-ansi.StringWidth(hint)-paneGap)
		question := ansi.Truncate(m.confirm.view(m.styles), room, "…")
		return question + strings.Repeat(" ", paneGap) + hint
	}
	if m.message != "" {
		return m.messageView()
	}
	if m.pending != "" {
		return m.styles.Info.Render(m.pending)
	}
	if m.zoomed {
		return m.hints(m.keys.DiffHelp())
	}
	switch {
	case m.branchesOpen:
		return m.hints(m.keys.BranchHelp())
	case m.view == viewLog:
		return m.hints(m.keys.LogHelp())
	}
	return m.hints(m.keys.ShortHelp())
}

// hints renders footer key hints that fit the width. Hints are ordered by
// importance, so the least important are dropped from the right; the last
// two (help and quit or back) always stay.
func (m Model) hints(bs []key.Binding) string {
	h := m.help
	h.Width = 0 // measure untruncated
	for len(bs) > 2 {
		if v := h.ShortHelpView(bs); ansi.StringWidth(v) <= m.innerWidth() {
			return v
		}
		bs = slices.Delete(slices.Clone(bs), len(bs)-3, len(bs)-2)
	}
	return h.ShortHelpView(bs)
}

func (m Model) messageView() string {
	if m.isErr {
		return m.styles.Error.Render(m.message)
	}
	return m.styles.Info.Render(m.message)
}
