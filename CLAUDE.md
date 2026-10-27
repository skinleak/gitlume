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
  ops.go                 Stage, Unstage, StageAll, Commit
internal/ui/
  app.go                 root Model: key routing, layout, header/footer, async orchestration
  msgs.go                tea.Msg types + tea.Cmd constructors wrapping internal/git
  status.go              fileList: grouped rows, cursor, scrolling, selection restore
  diff.go                diffView: renders only visible lines, truncates (never wraps)
  commit.go              commit prompt (textinput)
  help.go                help overlay generated from keys.KeyMap
  theme/                 the ONLY place colors and lipgloss styles are defined
  keys/                  the ONLY place keybindings are defined
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
4. Mutations go through `startOp`. Only one runs at a time (`busy`), so
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
  instead of separators. Added/removed colors are desaturated on purpose.
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
  `Repo.Diff` returns a `Note` for them instead.
- **The diff cache has a 32 MiB budget** (`maxDiffCacheBytes`) and is cleared
  on every status refresh.
- **`fileList` builds `rows`/`rowOf` once in `setChanges`.** Never rebuild
  them per keypress or frame; repos with huge untracked trees can have tens
  of thousands of rows.

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

Done: milestone 1 (status, diff, stage/unstage, stage all, commit, help).
Next: log view, then branches. Each gets its own model file in `internal/ui`
and its own git functions + tests in `internal/git`.
