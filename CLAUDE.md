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
  branches.go            for-each-ref and log parsers; Branches, Log, Graph, Show, Checkout
internal/ui/
  app.go                 root Model: key routing, layout, header/footer, async orchestration
  msgs.go                tea.Msg types + tea.Cmd constructors wrapping internal/git
  list.go                sectionList[T]: sectioned rows, cursor, scrolling (shared)
  status.go              fileList: change rows and selection restore
  diff.go                diffView: renders only visible lines, truncates (never wraps)
  branches.go            branchList + commitsView for the branch view (`2`)
  log.go                 logList (log `3` and graph `4`) and commit-header styling
  graph.go               lane layout and rendering for the graph view
  commit.go              commit prompt (textinput)
  confirm.go             y/n question guarding destructive operations
  help.go                help overlay generated from keys.KeyMap
  theme/                 the ONLY place colors and lipgloss styles are defined
  keys/                  the ONLY place keybindings are defined
tools/preview/           drives the real Model through a script, writes docs/preview.svg
.github/workflows/       release.yml: publishes pushed version tags to the Go proxy
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
   The graph view (`4`) is a second `logList` (`graph: true`) over all
   local and remote branches plus HEAD, and it is the whole page: no
   details pane, no diffstat, so `diffTarget()` has nothing for it and the
   page keys move the cursor. `loadGraph` lays out its lanes
   (`layoutGraph`) in the background along with the load; `View` only
   styles the cells of visible rows. `Model.history()` is the list of
   whichever of the two views is shown; each keeps its own `seq` and `limit`.
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
  red, modified/hashes/warnings yellow, remotes purple). The one exception
  is the graph's lanes, which cycle through `Palette.Lanes` so branches can
  be told apart.
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
  there and shows "No commits yet". The graph leaves HEAD out instead and
  still shows the other branches.
- **The graph needs full hashes and child-before-parent order.** Lanes are
  linked by `%H`/`%P` (abbreviations can differ in length), and
  `--date-order` guarantees children come first. A commit's parents that
  lie beyond the loaded page just leave lanes running off the bottom.
- **`%D` lists `refs/remotes/<remote>/HEAD`**, which `refLabels` leaves out.
- **Graph rows are painted with raw escape codes** (`lanePaints`), taken
  from rendering each lane style once per frame. A `Render` per colored run
  made a 40-lane frame 5× slower.
- **Destructive questions confirm only on `y`**, never on `enter`, so a
  stray keypress can't discard work.
- **The diff cache has a 32 MiB budget** (`maxDiffCacheBytes`) and is cleared
  on every status refresh.
- **`sectionList` builds `rows`/`rowOf` once in `setItems`.** Never rebuild
  them per keypress or frame; repos with huge untracked trees can have tens
  of thousands of rows. Every list view (files, branches, log, graph)
  embeds it and only adds rendering and selection restore.
- **Footer hints are ordered by importance.** `Model.hints` drops them from
  the right when they don't fit but always keeps the last two (help and
  quit/back), so put new bindings where they rank, not at the end.
- **Regenerate the README clip after visible UI changes** with
  `go run ./tools/preview`, and look at it: the script presses keys blindly,
  so a changed sort order or binding silently records the wrong thing. To
  read frames as text, drop a throwaway `_test.go` into `tools/preview`
  that runs `setupRepo` + `script` and prints `ansi.Strip(frame.ansi)`,
  then delete it. Keep the demo repo's dates so the branch view's order
  (main oldest of the local branches) still matches the script.

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
| Keypress + full frame at 220×60 with 50k files | ~0.3 ms | UI goroutine |
| One frame of a 1 MiB diff | ~50 µs, independent of diff size | UI goroutine |
| `splitDiff` of a 1 MiB diff | ~4 ms | background |
| `layoutGraph` of 3,000 commits on 40 lanes | ~2 ms | background |
| Keypress + frame of that graph | ~0.3 ms | UI goroutine |

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

Released as `v0.1.0`: milestone 1 (status, diff, stage/unstage, stage
all, commit, help), discard (file and all, behind a y/n question),
fullscreen diff (`enter`), branch view with checkout (`2`), log view (`3`),
full-page graph of all branches (`4`).

Each view gets its own model file in `internal/ui` and its own git
functions + tests in `internal/git`. There is nothing planned beyond this;
new features come from the owner. The views are deliberately few, so think
twice before suggesting a fifth tab.
