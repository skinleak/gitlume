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

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	chromeHeight = 5 // top margin, header, gap, gap, footer

	// maxDiffCacheBytes bounds memory used by cached diffs between refreshes.
	maxDiffCacheBytes = 32 << 20
	// maxCommitCacheEntries bounds the per-branch commit cache.
	maxCommitCacheEntries = 256

	// logPage is how many commits the log loads at a time; the next page
	// loads when the cursor gets within logPrefetch of the end.
	logPage     = 300
	logPrefetch = 50
)

// viewKind is the screen selected with the number keys.
type viewKind int

const (
	viewFiles viewKind = iota
	viewBranches
	viewLog
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
	promptKeys  keys.PromptKeyMap
	confirmKeys keys.ConfirmKeyMap
	styles      theme.Styles
	help        help.Model

	files    fileList
	diff     diffView
	branches branchList
	commits  commitsView
	log      logList
	commit   commitPrompt
	confirm  confirmPrompt

	branch    git.Branch
	statusSeq int
	diffSeq   int
	diffCache map[diffKey]diffContent
	cacheSize int // bytes held in diffCache

	branchSeq   int
	commitSeq   int
	commitCache map[string][]git.Commit // by full ref

	logSeq   int
	logLimit int // commits requested from git; grows by logPage

	view     viewKind
	focus    focusArea
	showHelp bool
	zoomed   bool // the diff fills the body
	busy     bool // a mutating git command is running
	message  string
	isErr    bool

	width, height int
}

// New returns the root model for repo.
func New(repo *git.Repo) Model {
	st := theme.Default()
	return Model{
		repo:        repo,
		keys:        keys.Default(),
		promptKeys:  keys.DefaultPrompt(),
		confirmKeys: keys.DefaultConfirm(),
		styles:      st,
		help:        newHelp(st),
		commit:      newCommitPrompt(st),
		diffCache:   make(map[diffKey]diffContent),
		commitCache: make(map[string][]git.Commit),
		logLimit:    logPage,
	}
}

// Init loads the initial status. Init cannot mutate the model, so it uses
// the current sequence number instead of calling refresh.
func (m Model) Init() tea.Cmd { return loadStatus(m.repo, m.statusSeq) }

// Update routes messages to handlers.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case opDoneMsg:
		m.busy = false
		m.setMessage(msg.info, msg.err)
		return m, m.refresh()
	}
	if m.commit.active {
		return m, m.commit.update(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.commit.active {
		return m, m.handleCommitKey(msg)
	}
	if m.confirm.active {
		return m, m.handleConfirmKey(msg)
	}
	m.message, m.isErr = "", false

	if m.showHelp {
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help, m.promptKeys.Cancel):
			m.showHelp = false
		}
		return m, nil
	}

	if m.zoomed {
		return m, m.handleZoomedKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Files):
		return m, m.showView(viewFiles)
	case key.Matches(msg, m.keys.Branches):
		return m, m.showView(viewBranches)
	case key.Matches(msg, m.keys.Log):
		return m, m.showView(viewLog)
	}
	switch m.view {
	case viewBranches:
		return m, m.handleBranchKey(msg)
	case viewLog:
		return m, m.handleLogKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
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
	}
	return m, nil
}

// handleLogKey handles keys in the log view. Like the files view, j/k move
// through commits or scroll the details, depending on focus.
func (m *Model) handleLogKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
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
	}
	return nil
}

func (m *Model) handleBranchKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
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
	}
	return nil
}

// showView switches screens. The branch list and the log are loaded every
// time they are shown, so they are never stale; the files view catches up
// on its diff.
func (m *Model) showView(v viewKind) tea.Cmd {
	if v == m.view {
		return nil
	}
	m.view, m.focus = v, focusFiles
	m.diffSeq++ // a diff still loading belongs to the previous view
	switch v {
	case viewBranches:
		return m.reloadBranches()
	case viewLog:
		return m.reloadLog()
	}
	return m.requestDiff()
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
	return m.startOp("Switching to "+name+"…", "Switched to "+name, func(ctx context.Context) error {
		return repo.Checkout(ctx, b)
	})
}

func (m *Model) handleCommitKey(msg tea.KeyMsg) tea.Cmd {
	m.message, m.isErr = "", false
	switch {
	case key.Matches(msg, m.promptKeys.Cancel):
		m.commit.close()
		return nil
	case key.Matches(msg, m.promptKeys.Confirm):
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

// handleZoomedKey handles keys while the diff is fullscreen. Only viewing
// keys work here, so nothing changes the repository by surprise.
func (m *Model) handleZoomedKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit
	case key.Matches(msg, m.keys.Back, m.keys.Open):
		m.setZoom(false)
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
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
	if m.view == viewLog {
		moved := m.log.move(delta)
		var more tea.Cmd
		if m.log.more && m.log.cursor >= len(m.log.items)-logPrefetch {
			m.log.more = false // one page request at a time
			m.logLimit += logPage
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
	return m.commit.open(m.commitInputWidth())
}

// startOp runs a mutating git command. Only one runs at a time so commands
// never contend for the index lock; keys pressed meanwhile are ignored.
func (m *Model) startOp(pending, done string, op func(context.Context) error) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	if pending != "" {
		m.setMessage(pending, nil)
	}
	return runOp(done, op)
}

func (m *Model) refresh() tea.Cmd {
	m.statusSeq++
	return loadStatus(m.repo, m.statusSeq)
}

func (m *Model) applyStatus(msg statusLoadedMsg) tea.Cmd {
	if msg.seq != m.statusSeq {
		return nil
	}
	if msg.err != nil {
		m.setMessage("", msg.err)
		return nil
	}
	m.branch = msg.status.Branch
	m.files.setChanges(allChanges(msg.status))
	m.clearDiffCache()
	if _, ok := m.diffTarget(); !ok && m.zoomed {
		m.setZoom(false)
	}
	switch m.view {
	case viewBranches:
		// The diff is loaded when the files view is shown again.
		return m.reloadBranches()
	case viewLog:
		return m.reloadLog()
	}
	return m.requestDiff()
}

// reloadLog loads the first logLimit commits of HEAD. A branch without
// commits has no log, and git would fail on it.
func (m *Model) reloadLog() tea.Cmd {
	m.logSeq++
	if m.branch.Unborn() {
		m.log.set(nil, false, m.log.now)
		return m.requestDiff()
	}
	return loadLog(m.repo, m.logSeq, m.logLimit)
}

func (m *Model) applyLog(msg logLoadedMsg) tea.Cmd {
	if msg.seq != m.logSeq {
		return nil
	}
	if msg.err != nil {
		m.setMessage("", msg.err)
		return nil
	}
	m.log.set(msg.commits, len(msg.commits) == msg.limit, msg.now)
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
	return loadCommits(m.repo, seq, b.Ref())
}

func (m *Model) applyCommits(msg commitsLoadedMsg) {
	if msg.seq != m.commitSeq {
		return
	}
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
// until the new one arrives, so the panel never flickers.
func (m *Model) requestDiff() tea.Cmd {
	m.diffSeq++
	k, ok := m.diffTarget()
	if !ok {
		m.diff.clear()
		return nil
	}
	if cached, ok := m.diffCache[k]; ok {
		m.diff.show(k, cached)
		return nil
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
	if k.commit != "" {
		return loadShow(m.repo, seq, k.commit, m.layout().diffW)
	}
	c, _ := m.files.selected()
	return loadDiff(m.repo, seq, c)
}

func (m *Model) applyDiff(msg diffLoadedMsg) {
	if msg.seq != m.diffSeq {
		return
	}
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

func (m *Model) setMessage(info string, err error) {
	if err != nil {
		m.message, m.isErr = err.Error(), true
		return
	}
	if info != "" {
		m.message, m.isErr = info, false
	}
}

// layout describes the pane sizes for the current window.
type layout struct {
	listW, listH int
	diffW, diffH int
	stacked      bool
}

func (m Model) innerWidth() int { return max(1, m.width-2*padX) }

func (m Model) layout() layout {
	w := m.innerWidth()
	bodyH := max(2, m.height-chromeHeight)
	if m.zoomed {
		return layout{diffW: w, diffH: bodyH}
	}
	if w < stackBelow {
		listH := max(1, bodyH*2/5)
		return layout{listW: w, listH: listH, diffW: w, diffH: max(1, bodyH-listH-1), stacked: true}
	}
	listW := clamp(w*35/100, minListWidth, maxListWidth)
	return layout{listW: listW, listH: bodyH, diffW: w - listW - paneGap, diffH: bodyH}
}

func (m *Model) resize() {
	l := m.layout()
	if !m.zoomed {
		// The lists keep their scroll position while hidden.
		m.files.setHeight(l.listH)
		m.branches.setHeight(l.listH)
		m.log.setHeight(l.listH)
		m.commits.setHeight(l.diffH)
	}
	m.diff.setHeight(l.diffH)
	m.help.Width = m.innerWidth()
	m.commit.input.Width = m.commitInputWidth()
}

// commitInputWidth leaves room on the footer line for the prompt label
// and its key hints. textinput renders one cell more than its Width for
// the cursor, hence the extra 1.
func (m Model) commitInputWidth() int {
	hint := ansi.StringWidth(m.help.ShortHelpView(m.promptKeys.ShortHelp()))
	return max(1, m.innerWidth()-ansi.StringWidth(commitLabel)-hint-paneGap-1)
}

// View renders the whole screen.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	l := m.layout()
	var body string
	if m.showHelp {
		body = m.helpOverlay(m.innerWidth(), m.height-chromeHeight)
	} else {
		body = m.bodyView(l)
	}
	page := lipgloss.JoinVertical(lipgloss.Left,
		"",
		m.headerView(),
		"",
		lipgloss.NewStyle().Height(max(2, m.height-chromeHeight)).MaxHeight(max(2, m.height-chromeHeight)).Render(body),
		"",
		m.footerView(),
	)
	// The clamp only matters for windows too small for the chrome itself.
	return lipgloss.NewStyle().Padding(0, padX).MaxWidth(m.width).MaxHeight(m.height).Render(page)
}

func (m Model) bodyView(l layout) string {
	if m.zoomed {
		return m.diff.view(m.styles, l.diffW)
	}
	var list, diff string
	switch m.view {
	case viewBranches:
		list = m.branches.view(m.styles, l.listW)
		diff = m.commits.view(m.styles, l.diffW)
	case viewLog:
		list = m.log.view(m.styles, l.listW, m.focus == focusFiles)
		diff = m.diff.view(m.styles, l.diffW)
	default:
		list = m.files.view(m.styles, l.listW, m.focus == focusFiles)
		diff = m.diff.view(m.styles, l.diffW)
	}
	list = lipgloss.NewStyle().Width(l.listW).Render(list)
	if l.stacked {
		list = lipgloss.NewStyle().Height(l.listH).Render(list)
		return lipgloss.JoinVertical(lipgloss.Left, list, "", diff)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Repeat(" ", paneGap), diff)
}

func (m Model) headerView() string {
	st := m.styles
	repo := st.Muted.Render(filepath.Base(m.repo.Root()))
	var branch string
	switch b := m.branch; {
	case b.Detached() && len(b.OID) >= 7:
		branch = st.Branch.Render("detached") + " " + st.Muted.Render(b.OID[:7])
	case b.Detached():
		branch = st.Branch.Render("detached")
	default:
		branch = st.Branch.Render(b.Head)
	}
	parts := []string{repo, branch}
	if m.branch.Unborn() && !m.branch.Detached() {
		parts = append(parts, st.Subtle.Render("no commits yet"))
	}
	if m.branch.Ahead > 0 {
		parts = append(parts, st.Ahead.Render("↑"+strconv.Itoa(m.branch.Ahead)))
	}
	if m.branch.Behind > 0 {
		parts = append(parts, st.Behind.Render("↓"+strconv.Itoa(m.branch.Behind)))
	}
	if m.zoomed {
		if c, ok := m.files.selected(); ok && m.view == viewFiles {
			parts = append(parts, st.Text.Render(displayPath(c)))
		}
		if c, ok := m.log.selected(); ok && m.view == viewLog {
			parts = append(parts, st.CommitHash.Render(sanitize(c.Hash))+" "+st.Text.Render(sanitize(c.Subject)))
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
	return tab(m.keys.Files, "Files", viewFiles) + "   " + tab(m.keys.Branches, "Branches", viewBranches) +
		"   " + tab(m.keys.Log, "Log", viewLog)
}

func (m Model) footerView() string {
	return ansi.Truncate(m.footerLine(), m.innerWidth(), "…")
}

func (m Model) footerLine() string {
	if m.commit.active {
		prompt := m.commit.view(m.styles)
		hint := m.help.ShortHelpView(m.promptKeys.ShortHelp())
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
	if m.zoomed {
		return m.hints(m.keys.DiffHelp())
	}
	switch m.view {
	case viewBranches:
		return m.hints(m.keys.BranchHelp())
	case viewLog:
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
