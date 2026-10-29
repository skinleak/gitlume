# CLAUDE.md

gitlume is a terminal git client in Go, inspired by lazygit but deliberately
smaller and calmer. Priorities, in order: **reliability, performance, a clean
minimal UI, a maintainable codebase**. Do not add features, options, or
dependencies that were not asked for.

## How we work

The owner (GitHub `skinleak/gitlume`) asks for features in short, casual
messages and often says "you decide". Follow this without being told:

- **Decide design details yourself** within the rules below and say in a
  sentence or two what you chose and why. Ask only for real product
  choices (where a feature lives, what a key does), with a recommendation.
- **Work on a branch, never on main.** Start from an up-to-date `main`:
  `feature/<name>` for features, `fix/<name>` for bugs, `ci/` and `docs/`
  for the rest. The owner reviews and merges pull requests on GitHub.
- **Commit, push and open the PR** (`gh pr create --base main`) once the
  change is done and verified, unless the owner says they'll commit it
  themselves. Commit messages are `type: lowercase summary` with `feat`,
  `fix`, `ui`, `ci` or `docs`. Never push to main, never force-push, and
  never merge a PR yourself.
- **A change is finished when**, besides the definition of done below:
  every bug fix and parser has a test, checked to fail without the fix;
  benchmarks touching a hot path stay in the performance budget; and the
  docs match the code: the README keys table and roadmap, this file's
  architecture, data flow, gotchas and roadmap, `keys.FullHelp()`, and the
  README clip (regenerated and checked frame by frame, see Gotchas).
- **After a PR is merged, offer a release** with the next version (see
  Releasing). Merged but untagged work never reaches users.
- **Report at the end:** what changed, any bug found along the way, what
  you chose not to do and why, the branch/PR link, and new files if the
  owner is committing.
- **When asked to review for bugs or bottlenecks**, fix what you find
  (with tests) instead of only listing it, and name what is left and why.

## Commands

```sh
go build ./...
go test ./...          # ~5s; integration tests create temp repos and need git
go vet ./...
gofmt -l .             # must print nothing
go run ./cmd/gitlume   # run inside any git repo
go test -run '^$' -bench . ./internal/...   # hot-path benchmarks
go run ./tools/preview # regenerate docs/preview.svg (README clip)
```

Definition of done for any change: `gofmt -l .` is empty and `go vet`,
`go build` and `go test` all pass. CI (`ci.yml`) checks the same on
Linux and macOS for every pull request; don't merge a red one.

## Stack

Go 1.27, Bubble Tea v1, Lip Gloss v1, Bubbles v1 (`key`, `help`,
`textinput`). Git is driven by shelling out to the `git` binary. Never use
go-git.

## Architecture

```
cmd/gitlume/main.go      Open repo → warm terminal background query → run tea.Program
internal/git/            UI-free git wrapper; everything here is unit-testable
  runner.go              the only place that executes git (fixed env, limits, *Error)
  runner_unix.go         detach: network commands run in their own session (no tty)
  runner_other.go        detach is a no-op where there is no controlling terminal
  repo.go                Open/ErrNotRepo, read (10s), write and network (5m) timeouts
  status.go              porcelain v2 -z parser; Status.Changes(group)
  diff.go                per-change diff, capped at MaxDiffBytes
  ops.go                 Stage, Unstage, StageAll, Commit, Discard, DiscardAll
  branches.go            for-each-ref and log parsers; Branches, Log, Graph, Show, Checkout
  sync.go                Push (porcelain parser, rejections) and Pull (fast-forward only)
internal/ui/
  app.go                 root Model: key routing, layout, header/footer, async orchestration
  msgs.go                tea.Msg types + tea.Cmd constructors wrapping internal/git;
                         `request` cancels a superseded load's git process
  list.go                sectionList[T]: sectioned rows, cursor, scrolling (shared)
  status.go              fileList: change rows and selection restore
  diff.go                diffView: renders only visible lines, truncates (never wraps);
                         tidyPatch: one header line per file instead of git's
  branches.go            branchList + commitsView: the branch list (`b`) over files or log
  log.go                 logList (the log `2`: all branches as a graph) and commit-header styling
  graph.go               lane layout and rendering for the log's graph
  commit.go              commit prompt (textinput)
  confirm.go             y/n question guarding destructive operations
  help.go                help overlay generated from keys.KeyMap; reflows and scrolls
  theme/                 the ONLY place colors and lipgloss styles are defined
  keys/                  the ONLY place keybindings are defined
tools/preview/           drives the real Model through a script, writes docs/preview.svg
.github/workflows/       ci.yml: fmt, vet, build (+ Windows) and tests on Linux and macOS
                         for every PR; release.yml: publishes version tags to the Go proxy
```

### Data flow

1. A key press, `tea.FocusMsg` or finished operation leads to `refresh()`.
   Only one status load runs at a time (`loading`): a refresh asked for
   meanwhile sets `reload` and starts when the running one is applied.
2. Polling: `Init` starts a `pollTickMsg` chain. A tick loads the status
   in the background unless a load, operation or prompt is active or a
   status arrived within half an interval. The interval is 50× the last
   status's run time, between 2 s and 60 s, so polling stays under ~2%
   of a core in any repo. A poll compares the result with `Model.status`
   inside the `tea.Cmd`; when nothing changed it only reloads the file
   diff on screen (an already modified file can change without changing
   the status) and leaves the other views alone.
3. A changed `statusLoadedMsg` is applied: the file list keeps the
   selection, the diff cache is cleared, and `requestDiff()` runs.
4. `requestDiff()` serves the diff from `diffCache` if it can. Otherwise it
   schedules a 30 ms debounced `diffTickMsg`, and that triggers `loadDiff`.
   Results whose `seq` doesn't match are dropped, and the old diff stays
   visible until the new one arrives, so there's no flicker; a diff of the
   other kind (a file diff in the log, a commit in the files view) is
   cleared instead, because it would be wrong rather than old. Every new
   request cancels the running load's git process (`diffReq`), since key
   repeat (~33 ms) outpaces the debounce. Commits (`commitReq`) and
   histories (`logList.req`) are cancelled the same way.
5. The branch list (`b`) is not a tab: `branchesOpen` covers the files or
   log view until a checkout, `esc` or a tab key, and `showView` then
   reloads the view beneath. It loads branches every time it opens and
   after every status refresh while it is open (`branchSeq`). The selected
   branch's commits load like diffs: debounced, `commitSeq`-checked, cached
   per ref and cleared with each branch reload. The files view requests its
   diff again when it is shown.
6. The log view (`2`) is the history of all local and remote branches
   plus HEAD, drawn as a graph, loaded in pages of `logPage` commits
   (`logList.seq`); the next page loads when the cursor nears the end.
   `loadLog` lays out the lanes (`layoutGraph`) in the background along
   with the load; `View` only styles the cells of visible rows. Each row
   is lanes, hash, refs and subject. The selected commit is shown in the
   diff pane through the same `requestDiff`/`diffCache` path as file
   diffs, keyed by `diffKey{commit: hash}`; `diffTarget()` decides what
   the pane shows. `Model.history()` is the log's list while it is shown.
   A reload keeps the cursor on its commit, except that it goes to HEAD
   when HEAD's branch changed (`logList.branch`) and follows HEAD when
   HEAD was selected; HEAD moving otherwise never moves the cursor, so a
   commit made elsewhere doesn't pull someone away from old history.
7. Mutations go through `start` (`startOp` for plain ones, `runSync` for
   push and pull). Only one runs at a time (`busy`), so commands never
   fight over `index.lock`. Its `pending` text ("Pushing…") stays in the
   footer until it ends; messages clear on the next key, which would hide
   a slow push. `opDoneMsg` always triggers a refresh, even after an error.

## Rules

- **All git calls go through `run()` in `internal/git/runner.go`.** It sets
  `LC_ALL=C`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`,
  `GIT_LITERAL_PATHSPECS=1`, `color.ui=false` and `core.quotepath=false`.
  Prefer machine formats (`--porcelain=v2 -z`, `-z` in general) over human
  output. Always put `--` before paths.
- **Untrusted text goes through `sanitize()` before display.** That covers
  file contents and file names, which can contain escape sequences.
- **No UI code in `internal/git`**, and no `os/exec` in `internal/ui`.
- **Every git command runs as a `tea.Cmd`.** Never call git inside `Update` or
  `View`.
- **Errors never crash the app.** Send them to `setMessage("", err)` so they
  appear in the footer status line. They clear on the next key press.
  Info messages (`setMessage(info, nil)`) also fade after `infoTimeout`
  (3 s), so the key hints come back; errors don't, since they can quote
  hook output that takes longer to read.
- **Colors and styles live only in `theme`; bindings live only in `keys`.**
  Add a binding to `KeyMap` and to `FullHelp()`, and the help overlay picks
  it up.
- **Keep the look calm:** no borders or boxes, one accent color, spacing
  instead of separators. Colors are bright ANSI indices (SGR 90–97) so the
  terminal theme decides the exact shades. Blue is the UI accent; the other
  colors carry git meaning only (added/current/ahead green, removed/behind
  red, modified/hashes/warnings yellow, remotes purple). The one exception
  is the graph's lanes, which cycle through `Palette.Lanes` so branches can
  be told apart; the first, the accent, is reserved for HEAD's line
  (`headColor`). Directories are muted next to bright file names.
- **Status letters follow GitHub/VS Code, not raw git:** untracked shows `U`
  and conflicts `!` (`displayCode`). `git.Change.Code` keeps git's letters;
  only the UI maps them.
- **No global mutable state.** Everything hangs off `Model` or `Repo`.
- **Write a test for every new parser.** Capture real git output from a temp
  repo for fixtures rather than writing it from memory; format details such
  as the rename source path being its own `-z` field are easy to get wrong.

## Gotchas (learned the hard way)

- **`Init()` has a value receiver**, so changes it makes to the model are
  lost. That's why `Init` calls `loadStatus(m.repo, m.statusSeq)` directly
  instead of `refresh()`.
- **Don't use `bubbles/viewport` for diffs.** It applies `Width()`, which
  wraps long lines. `diffView` truncates with `ansi.Truncate` and styles only
  the visible lines, so very large diffs stay cheap.
- **`help.ShortHelpView` can exceed `help.Width`** when not even the "…"
  fits, so the footer is truncated explicitly.
- **`lipgloss.HasDarkBackground()` must run in `main` before
  `tea.NewProgram`.** Otherwise Bubble Tea's input reader can swallow the
  terminal's reply and startup stalls for up to 5 s.
- **The textinput cursor is static on purpose:** it's calmer, it avoids a
  redraw every 530 ms, and it stops the test harness from looping on blink
  messages.
- **`git reset -q -- <paths>` is how unstaging works.** It also works on an
  unborn branch, where `git restore --staged` fails. To unstage a staged
  rename, pass both the new and the old path.
- **Untracked diffs use `git diff --no-index /dev/null <path>`.** Exit code 1
  is the normal result there and is listed in `okCodes`.
- **Diff lines are sanitized in `splitDiff`:** tabs become 4 spaces, `\r`
  is dropped, and other control characters become `?`. Without this, width
  math breaks and file contents could inject terminal escape sequences.
- **Never embed `bytes.Buffer` in a custom `io.Writer`.** The embedded
  `ReadFrom` is promoted, `io.Copy` (used by `os/exec`) prefers it, and it
  silently bypasses your `Write`. That's why `cappedBuffer.buf` is a named
  field.
- **`cmd.WaitDelay` is required.** A commit hook can leave a background
  process holding git's stdout open. Without `WaitDelay`, `Run` would never
  return and the UI would stay `busy` forever.
- **Git puts some failures on stdout**, such as "nothing added to commit".
  `Error.Error()` uses the first `fatal:`/`error:` line of stderr, or else the
  last line of stdout.
- **Untracked nested repositories** appear as `path/` and have no diff.
  `Repo.Diff` returns a `Note` for them instead, and `Repo.Discard` refuses
  to delete them (`ErrNestedRepo`).
- **Discarding a file uses `git restore --source=HEAD --staged --worktree`**,
  which fails on an unborn branch, so `Discard` falls back to `git rm -f`
  there. A staged rename must be restored together with its source path,
  even from the unstaged row. `git reset --hard` does work on an unborn
  branch, so `DiscardAll` needs no special case.
- **Branch ages are relative to `branchList.now`**, captured when the list
  loads, so `View` never calls `time.Now()` and frames are deterministic.
- **Remote checkout uses `git switch --track <remote>/<name>`.** When a local
  branch of that name already exists, the UI switches to it instead
  (`branchList.localFor`), because git would refuse to create it again.
- **Commit details are `git show --decorate=full`.** Full ref names tell
  local from remote branches; `styleRefs` shortens them while styling, so
  header lines are styled first and truncated after (`diffContent.header`).
  Patch lines keep the cheap truncate-then-style path.
- **User config can inject lines into machine output.** `log.showSignature`
  adds signature lines to `git log --format=...`, which broke parsing, so
  `Log` and `Show` pass `--no-show-signature`. `diff.noprefix` and
  `diff.mnemonicPrefix` change the `a/`/`b/` path prefixes `tidyPatch`
  reads, so `Diff` and `Show` pin them (`diffPrefixes`). Check new commands
  against config that changes output, not just flags.
- **Patches are tidied in the background** (`tidyPatch`, after
  `splitDiff`): each file's `diff --git`/`index`/`---`/`+++`/mode/rename
  lines become one `fileHead` (name, note, +/- counts), with a blank line
  between files. It tracks header and hunk state, so a removed line that
  starts with `--` stays red; `styleDiffLine` no longer special-cases
  `---`/`+++`. Unknown header lines are kept. A truncated patch shows no
  counts for its last file. Combined diffs (`diff --cc`, `@@@`) have one
  +/- column per parent.
- **Lip Gloss renders no colors in tests** (no terminal), so a style
  assertion is trivially true. Call `withColors(t)` in tests that compare
  styles, and check a test fails without its fix.
- **`git log HEAD` fails on an unborn branch**, so the log leaves HEAD
  out there (`Repo.Graph`'s `head`) and still shows the other branches.
  Only a repository without any commits shows "No commits yet".
- **The graph needs full hashes and child-before-parent order.** Lanes are
  linked by `%H`/`%P` (abbreviations can differ in length), and
  `--date-order` guarantees children come first. A commit's parents that
  lie beyond the loaded page just leave lanes running off the bottom.
- **`%D` lists `refs/remotes/<remote>/HEAD`**, which `refLabels` leaves out.
- **Graph rows and file list rows are painted with raw escape codes**
  (`paint`, `paintOf`), taken from rendering each style once per frame. A
  `Render` per colored run made a 40-lane frame 5× slower, and one per
  muted directory made the file list frame 1.5× slower.
- **Log rows line up after the widest graph on screen** (`graphW`), capped
  at half the row so one very wide merge can't push every subject off.
  Nodes are `◉` HEAD, `○` merge, `●` other.
- **Network commands run without a controlling terminal** (`detach`,
  `setsid`). `GIT_TERMINAL_PROMPT=0` stops git's own prompts, but ssh and
  some credential helpers open `/dev/tty` directly for a passphrase or a
  host key, underneath the UI and racing Bubble Tea for keystrokes.
  Without a terminal they fail and the error reaches the footer. Cancel
  kills the whole process group, so ssh dies with git. The test compares
  process groups (macOS `ps` has no session ids), so it means the same
  with or without a tty.
- **Push never forces and pull only fast-forwards.** Pull is
  `pull --ff-only --no-rebase`, so `pull.rebase` can't turn it into a
  rebase on any git version. Push uses `--porcelain` to tell up to date
  (`=`), rejected because the remote moved on (`ErrRejected`) and refused
  by the remote, such as a hook (quoting its `remote:` line), apart. The
  first push picks the remote like git: `%(push:remotename)`, then
  origin, then the only remote. Branch state is read fresh by
  `currentBranch`, not taken from a status that may be seconds old.
- **Destructive questions confirm only on `y`**, never on `enter`, so a
  stray keypress can't discard work.
- **The diff cache has a 32 MiB budget** (`maxDiffCacheBytes`) and is cleared
  on every status refresh.
- **`sectionList` builds `rows`/`rowOf` once in `setItems`.** Never rebuild
  them per keypress or frame; repos with huge untracked trees can have tens
  of thousands of rows. Every list view (files, branches, log) embeds
  it and only adds rendering and selection restore.
- **`View` assembles the frame line by line** (`bodyLines`, `fit`) from
  parts already cut to their width. Lip Gloss's `Width`, `Height`,
  `Padding` and `Join*` measure every cell of the frame again; they made a
  frame 3× slower and allocated 520 KB. Lines are not padded on the right:
  Bubble Tea's renderer erases the rest of each line itself.
- **`resize` sizes every view, not only the shown one**, because switching
  views doesn't resize. Only list widths differ between views (the log's
  list gets `logListShare` of the width for its lanes, hash and refs), and
  they are applied in `View`, so the heights `resize` sets fit every view.
- **Polling and fading messages sleep in `tea.Tick`**, so code that drives
  the model synchronously (tests, `tools/preview`) must use
  `New(repo).WithoutPolling()`, which turns both off, and send `pollTickMsg`
  or `fadeMsg` itself when it wants one. `Update` schedules the fade for
  any handler that set a message (`messageSeq` changed), so `setMessage`
  needs no `tea.Cmd` of its own, and a stale fade never clears a newer
  message.
- **The help screen never cuts a key off.** `help.FullHelpView` with a
  `Width` drops whole groups behind a `…`, which hid `b` and the tabs at
  60 columns, so `helpLines` renders each group on its own and puts as
  many side by side as fit, the rest below. Taller than the window, it
  scrolls with j/k (`helpOffset`), with the hint kept on the last line.
  `TestHelpShowsEveryKey` checks every binding can be reached in split
  windows down to 34×10.
- **Messages are sanitized in `setMessage`.** Errors quote hook output,
  which is often colored, and a raw escape code would reach the terminal.
- **Footer hints are ordered by importance.** `Model.hints` drops them from
  the right when they don't fit but always keeps the last two (help and
  quit/back), so put new bindings where they rank, not at the end.
- **Regenerate the README clip after visible UI changes** with
  `go run ./tools/preview`, and look at it: the script presses keys blindly,
  so a changed sort order or binding silently records the wrong thing. To
  read frames as text, drop a throwaway `_test.go` into `tools/preview`
  that runs `setupRepo` + `script` and prints `ansi.Strip(frame.ansi)`,
  then delete it. Keep the demo repo's dates so the branch list's order
  (main oldest of the local branches) still matches the script. Commit
  hashes and dates differ on every run, so a regenerated clip always
  differs; only commit it when frames actually changed.

## Releasing

Users install with `go install .../cmd/gitlume@latest`, which resolves to
the highest `vX.Y.Z` tag. Untagged commits on main never reach them, and the
Go proxy caches `@latest`, so a release is a tag:

```sh
git tag -a v0.2.0 -m "v0.2.0: <what changed>"
git push origin v0.2.0
```

`.github/workflows/release.yml` then asks proxy.golang.org for the tag, which
makes it `@latest` for everyone at once. Bump the minor version for
features and the patch version for fixes; `v0.x` means no compatibility
promises yet. Tag merged commits on `main` only, after `git pull`, and
check with `go list -m github.com/skinleak/gitlume@latest` (run outside
the repo) that the proxy serves the new version.

Never tell users to install `@main`: it ships unreleased work, and the
README command must stay `@latest`.

## Performance budget

These are measured with the benchmarks in `*/bench_test.go`. Keep them in
this range:

| Path | Cost | Runs on |
|---|---|---|
| Parse a 50k-entry status | ~10 ms | background |
| Keypress + file list frame with 50k files | ~50 µs | UI goroutine |
| Keypress + full frame at 220×60 with 50k files and a diff | ~0.2 ms | UI goroutine |
| One frame of a 1 MiB diff | ~50 µs, independent of diff size | UI goroutine |
| `splitDiff` + `tidyPatch` of a 1 MiB diff | ~5 ms | background |
| `layoutGraph` of 3,000 commits on 40 lanes | ~2 ms | background |
| Keypress + log list frame of that graph | ~0.3 ms | UI goroutine |

Anything proportional to repo or diff size belongs in a `tea.Cmd`, not in
`Update` or `View`. Background polling must stay under ~2% of a core
(`pollCost`), and an unchanged poll must not reload anything but the
visible file diff.

## Testing

- `internal/git/status_test.go`: table tests for the porcelain parser,
  including renames, unicode, spaces, unborn and detached HEAD, conflicts and
  malformed input.
- `internal/git/repo_test.go`: integration tests against isolated temp repos
  (`GIT_CONFIG_GLOBAL=/dev/null`).
- `internal/ui/app_test.go`: drives the real `Model` headlessly with
  `pump`/`drain`. `TestViewFitsWindow` checks that every frame is exactly the
  terminal height and never wider than the terminal, in several sizes and
  modes. Keep it passing whenever you touch layout.

## Roadmap

Released as `v0.1.0`: milestone 1 (status, diff, stage/unstage, stage
all, commit, help), discard (file and all, behind a y/n question),
fullscreen diff (`enter`), branch view with checkout (`2`), log view (`3`),
full-page graph of all branches (`4`).

Released as `v0.2.0`: switching tabs with `←`/`→`.

Released as `v0.3.0`: background status polling, coalesced refreshes,
cancelled stale loads, a 3× cheaper frame, and the fixes from the
reliability review.

Released as `v0.4.0`: the log and the graph merged into one tab:
the graph of all branches with hashes, next to the selected commit's
details and diff. The branch view is no longer a tab but a list opened
with `b` over files or the log. Two tabs remain: files `1`, log `2`.
UI polish: one header line per file in diffs, muted directories, graph
columns aligned, `◉`/`○` nodes and HEAD's line in the accent color.

Released as `v0.5.0`: push (`P`) and pull (`p`), in the files and log
views. The help screen shows every key in small windows: groups that
don't fit side by side go below each other, and it scrolls with j/k.

Released as `v0.5.1`: info messages fade after 3 seconds so the
key hints come back; errors still stay until the next key.

Unreleased (next: `v0.5.2`): after a checkout the log selects the new
HEAD, and follows HEAD when it was selected. "Nothing to push" says when
the changes just aren't committed yet.

Check `git ls-remote --tags origin` before picking a version; the roadmap
can lag behind the tags.

Each view gets its own model file in `internal/ui` and its own git
functions + tests in `internal/git`. There is nothing planned beyond this;
new features come from the owner. The views are deliberately few, so think
twice before suggesting a third tab.
