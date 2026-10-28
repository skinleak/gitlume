# gitlume

A calm, minimal terminal UI for git.

gitlume shows what changed, lets you stage, discard and commit it, and
switch branches. No
boxes, no clutter: one accent color, generous spacing, and keybindings you
can discover with `?`.

![gitlume: browsing changes, viewing a diff fullscreen, staging, discarding and committing](docs/preview.svg)

## Install

```sh
go install github.com/skinleak/gitlume/cmd/gitlume@latest
```

You need Go 1.27 or newer to build it, and `git` on your `PATH` to run it.

## Usage

Run `gitlume` anywhere inside a git work tree.

| Key              | Action                                |
| ---------------- | ------------------------------------- |
| `j` / `k`, `↓` `↑` | Move through files (or scroll the diff) |
| `tab`            | Switch focus between files and diff   |
| `ctrl+d` / `ctrl+u` | Scroll the diff by half a page      |
| `enter`          | View the selected diff fullscreen (`esc` to go back) |
| `space`          | Stage or unstage the selected file    |
| `a`              | Stage everything                      |
| `d`              | Discard all changes to the selected file (asks first) |
| `D`              | Discard all uncommitted changes, including untracked files (asks first) |
| `c`              | Commit (`enter` to confirm, `esc` to cancel) |
| `1` / `2`        | Switch between files and branches     |
| `enter` (branches) | Check out the selected branch (a remote branch gets a local tracking branch) |
| `r`              | Refresh                               |
| `?`              | Show all keybindings                  |
| `q`              | Quit                                  |

Status refreshes after every action and whenever the terminal window
regains focus.

## Design

- **Real git, not a reimplementation.** gitlume runs the `git` binary and
  parses its stable porcelain formats. Your config, hooks, and credentials
  behave exactly as they do on the command line.
- **Never blocks.** Every git command runs in the background. Results that
  arrive out of order are dropped, so fast navigation never shows the
  wrong diff.
- **Never fights you for the lock.** Background refreshes use
  `GIT_OPTIONAL_LOCKS=0`, and write operations run one at a time.
- **Stays fast in big repos.** Diffs are capped at 1 MiB and only the
  visible lines are rendered. A keypress takes about 0.3 ms even with
  50,000 changed files.
- **Safe with odd file names and contents.** Paths are passed literally (a
  file named `*.txt` is just that file), and control characters can't reach
  your terminal.
- **Never crashes on a git error.** Failures show up in the status line.

## Development

```sh
go build ./...        # build
go test ./...         # unit + integration tests (needs git)
go vet ./...
go run ./cmd/gitlume  # run against the current repository
go run ./tools/preview  # regenerate docs/preview.svg after UI changes
```

```
cmd/gitlume/          entry point
internal/git/         git CLI wrapper and porcelain parsers (no UI)
internal/ui/          Bubble Tea models: root app, file list, diff, commit prompt, help
internal/ui/theme/    every color and style
internal/ui/keys/     every keybinding (help is generated from these)
tools/preview/        records the README clip from the real UI
```

## Roadmap

Done: status, diff, stage, discard, commit, and the branch view. Next up:
the log view.
