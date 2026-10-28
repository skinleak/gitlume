# CLAUDE.md

gitlume is a terminal git client in Go, inspired by lazygit but deliberately
smaller and calmer. Priorities, in order: **reliability, performance, a clean
minimal UI, a maintainable codebase**. Do not add features, options, or
dependencies that were not asked for.

## Commands

```sh
go build ./...
go test ./...          # ~1s; integration tests create temp repos and need git
go vet ./...
gofmt -l .             # must print nothing
go run ./cmd/gitlume   # run inside any git repo
go test -run '^$' -bench . ./internal/...   # hot-path benchmarks
go run ./tools/preview # regenerate docs/preview.svg (README clip)
```

Definition of done for any change: `gofmt -l .` is empty and `go vet`,
`go build` and `go test` all pass.

## Stack

Go 1.27, Bubble Tea v1, Lip Gloss v1, Bubbles v1 (`key`, `help`,
`textinput`). Git is driven by shelling out to the `git` binary. Never use
go-git.

## Architecture

```
cmd/gitlume/main.go      Open repo → warm terminal background query → run tea.Program
internal/git/            UI-free git wrapper; everything here is unit-testable
  runner.go              the only place that executes git (fixed env, limits, *Error)
  repo.go                Open/ErrNotRepo, read (10s) and write (5m) timeouts
  status.go              porcelain v2 -z parser; Status.Changes(group)
  diff.go                per-change diff, capped at MaxDiffBytes
  ops.go                 Stage, Unstage, StageAll, Commit, Discard, DiscardAll
  branches.go            for-each-ref and log parsers; Branches, Log, Show, Checkout
internal/ui/
  app.go                 root Model: key routing, layout, header/footer, async orchestration
  msgs.go                tea.Msg types + tea.Cmd constructors wrapping internal/git
  list.go                sectionList[T]: sectioned rows, cursor, scrolling (shared)
  status.go              fileList: change rows and selection restore
  diff.go                diffView: renders only visible lines, truncates (never wraps)
  branches.go            branchList + commitsView for the branch view (`2`)
  log.go                 logList and commit-header styling for the log view (`3`)
  commit.go              commit prompt (textinput)
  confirm.go             y/n question guarding destructive operations
  help.go                help overlay generated from keys.KeyMap
  theme/                 the ONLY place colors and lipgloss styles are defined
  keys/                  the ONLY place keybindings are defined
tools/preview/           drives the real Model through a script, writes docs/preview.svg
```

### Data flow

1. A key press or `tea.FocusMsg` leads to `refresh()`, which increments
   `statusSeq` and returns `loadStatus`.
2. `statusLoadedMsg` is applied only if its `seq` matches. The file list keeps
   the selection, the diff cache is cleared, and `requestDiff()` runs.
3. `requestDiff()` serves the diff from `diffCache` if it can. Otherwise it
   schedules a 30 ms debounced `diffTickMsg`, and that triggers `loadDiff`.
   Results whose `seq` doesn't match are dropped, and the old diff stays
   visible until the new one arrives, so there's no flicker.
4. The branch view (`2`) loads branches every time it is shown and after
   every status refresh while it is visible (`branchSeq`). The selected
   branch's commits load like diffs: debounced, `commitSeq`-checked, cached
   per ref and cleared with each branch reload. The files view requests its
   diff again when it is shown.
5. The log view (`3`) loads HEAD's history in pages of `logPage` commits
   (`logSeq`); the next page loads when the cursor nears the end. The
   selected commit is shown in the diff pane through the same
   `requestDiff`/`diffCache` path as file diffs, keyed by
   `diffKey{commit: hash}`; `diffTarget()` decides what the pane shows.
6. Mutations go through `startOp`. Only one runs at a time (`busy`), so
   commands never fight over `index.lock`. `opDoneMsg` always triggers a
   refresh, even after an error.

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
- **Colors and styles live only in `theme`; bindings live only in `keys`.**
  Add a binding to `KeyMap` and to `FullHelp()`, and the help overlay picks
  it up.
- **Keep the look calm:** no borders or boxes, one accent color, spacing
  instead of separators. Colors are bright ANSI indices (SGR 90–97) so the
  terminal theme decides the exact shades. Blue is the UI accent; the other
  colors carry git meaning only (added/current/ahead green, removed/behind
  red, modified/hashes/warnings yellow, remotes purple).
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
  `Log` and `Show` pass `--no-show-signature`. Check new commands against
  config that changes output, not just flags.
- **Lip Gloss renders no colors in tests** (no terminal), so a style
  assertion is trivially true. Call `withColors(t)` in tests that compare
  styles, and check a test fails without its fix.
- **`git log HEAD` fails on an unborn branch**, so `reloadLog` skips git
  there and shows "No commits yet".
- **Destructive questions confirm only on `y`**, never on `enter`, so a
  stray keypress can't discard work.
- **The diff cache has a 32 MiB budget** (`maxDiffCacheBytes`) and is cleared
  on every status refresh.
- **`sectionList` builds `rows`/`rowOf` once in `setItems`.** Never rebuild
  them per keypress or frame; repos with huge untracked trees can have tens
  of thousands of rows. Every list view (files, branches, the log later)
  embeds it and only adds rendering and selection restore.
- **Footer hints are ordered by importance.** `Model.hints` drops them from
  the right when they don't fit but always keeps the last two (help and
  quit/back), so put new bindings where they rank, not at the end.
- **Regenerate the README clip after visible UI changes** with
  `go run ./tools/preview`, and look at it: the script presses keys blindly,
  so a changed sort order or binding silently records the wrong thing.

## Performance budget

These are measured with the benchmarks in `*/bench_test.go`. Keep them in
this range:

| Path | Cost | Runs on |
|---|---|---|
| Parse a 50k-entry status | ~10 ms | background |
| Keypress + full frame at 220×60 with 50k files | ~0.3 ms | UI goroutine |
| One frame of a 1 MiB diff | ~50 µs, independent of diff size | UI goroutine |
| `splitDiff` of a 1 MiB diff | ~4 ms | background |

Anything proportional to repo or diff size belongs in a `tea.Cmd`, not in
`Update` or `View`.

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

Done: milestone 1 (status, diff, stage/unstage, stage all, commit, help),
discard (file and all, behind a y/n question), fullscreen diff (`enter`),
branch view with checkout (`2`), log view (`3`). Each view gets its own
model file in `internal/ui` and its own git functions + tests in
`internal/git`.
