# wrktree: design

Date: 2026-11-24
Status: draft, waits for approval

## Intent

`git worktree list` prints long paths that wrap in the terminal. To delete a
worktree, the user must copy a full path. The output does not show disk size,
changed files, or how old a worktree is. A user with 10 worktrees of a monorepo
cannot decide quickly which worktrees to keep.

`wrktree` is a small CLI that shows all worktrees of a repository in one table
and lets the user act on a worktree with one key or one short command.

The tool must stay lightweight. The user examined Worktrunk and found it
complicated. Each feature must earn its place.

## Goals

- Show each worktree with its branch, changed files, distance from the base
  branch, disk size, and last commit time.
- Switch the shell to a worktree.
- Open a worktree in an app. The app is a configuration value.
- Delete a worktree safely.
- Prune stale worktree entries.
- Supply an interactive screen and subcommands. The two use the same functions.
- Start fast. The table shows in less than 100 ms. Slow values fill in later.

## Non-goals (version 1)

- Creation of worktrees. `git worktree add` stays the command for that.
- Windows support. macOS and Linux only.
- Fish shell support. zsh and bash only.
- Management of many repositories at one time. The tool works on the
  repository of the current directory.

## Anti-goals

- The tool must not delete work without a clear confirmation.
- The tool must not need a configuration file to operate.
- The tool must not hide git. Each action maps to a git command that the user
  knows.

## Constraints

- A child process cannot change the directory of the parent shell. Switch
  needs a shell function.
- Disk size of a monorepo worktree with `node_modules` takes seconds to
  calculate.
- The binary name is `wrktree`. The name `wt` collides with other tools, so
  `wt` is only an optional shell function name.

## Approaches

**A. Bubble Tea screen, git CLI for data. Selected.**
The tool runs `git` commands and reads porcelain output. Bubble Tea draws the
screen. Cobra supplies the subcommands. The git CLI is the source of truth, so
the tool behaves the same as the commands the user types now. Cost: 3
dependencies.

**B. `fzf` as the screen.** Less code. Rejected: each user must install `fzf`,
the size column cannot update while the list shows, and confirmation prompts
are awkward.

**C. Bubble Tea screen, `go-git` for data.** No `git` binary necessary.
Rejected: `go-git` has weak worktree support and slow status on a monorepo.

The user selected A.

## Decisions of the user

- Language: Go.
- Binary name: `wrktree`. Short name `wt` through a shell function.
- Interactive screen and subcommands, the two together.
- The open command is a configuration value. The default is VS Code (`code`).
- The table shows how far each worktree is behind `origin/master`.

## Design

### Commands

| Command | Action |
|---|---|
| `wrktree` | Open the interactive screen. |
| `wrktree ls [--json] [--fetch] [--fresh]` | Print the table one time, with all values. |
| `wrktree switch <name>` | Switch the shell to the worktree. |
| `wrktree open <name>` | Run the open command on the worktree. |
| `wrktree rm <name> [--force] [--branch]` | Delete the worktree. |
| `wrktree prune [--yes]` | Delete stale worktree entries. |
| `wrktree init <zsh\|bash>` | Print the shell function. |

`<name>` matches a substring of the branch name or of the folder name. The
match ignores case. An exact match of the branch name or the folder name wins
over substring matches. Thus `master` selects `master` when `master-fix` also
exists. If there is no exact match and more than one worktree matches, the
command prints the matches and stops with exit code 1. (Builder call.)

If the current directory is not in a git repository, each command prints
`wrktree: not a git repository` and stops with exit code 1.

### Screen

```
 wrktree · levelsfyi-mono                                  10 worktrees · 14.2 GB

   BRANCH                               CHANGES    origin/master   SIZE     LAST COMMIT
 ● master                               clean      ↑0 ↓3           3.1 GB   2 hours ago
 › eng-6201-onboarding-v2-alerts        4 files    ↑2 ↓31          1.4 GB   3 weeks ago
   eng-6458-bugsink-key-flows ✓ merged  clean      ↑1 ↓12          1.2 GB   9 days ago
   eng-6882-tp-self-serve-ui            12 files   ↑5 ↓0           …        1 day ago

 enter switch · o open · c copy · d delete · D delete + branch · p prune · f fetch · / filter · q quit
```

- `●` marks the worktree of the current directory. `›` marks the cursor.
- The cursor row is bold, in ANSI color 6 (cyan). The row has one color:
  cells that are dim on other rows are not dim on the cursor row. The other
  rows do not change.
- The main worktree is always the first row. The other rows sort by last
  commit time, newest first.
- A locked worktree shows a lock mark after the branch name.
- A detached worktree shows the short commit hash in place of the branch.
- If the branch name does not match the folder name, the folder name shows
  dimmed after the branch name.
- A branch that the base branch already contains shows a green `✓ merged`
  mark after the branch name and the folder name, before `[locked]` and
  `[missing]`. A branch with no commits of its own shows a dim `no commits`
  mark at the same position. On the cursor row, the mark has the row color.
  When the name is cut for the width, the marks stay. The main worktree never
  shows a mark.

Keys:

| Key | Action |
|---|---|
| `↑` `↓` `←` `→` `j` `k` | Move the cursor. `←` moves up and `→` moves down. The cursor wraps from the first row to the last row and back. |
| `enter` | Switch to the worktree and quit. |
| `o` | Quit, then run the open command on the worktree. |
| `c` | Copy the branch name to the system clipboard. A detached worktree gives the 7-character head. |
| `d` | Delete the worktree. The branch stays. A clean worktree has no prompt. |
| `D` | Delete the worktree, then its branch with `git branch -d`. |
| `p` | Show stale entries, then prune after confirmation. |
| `f` | Run `git fetch origin`, then calculate the distances again. |
| `/` | Filter the rows by text. |
| `r` | Read all values again. |
| `q` `esc` | Quit. While a delete runs, these keys do not quit. |
| `ctrl+c` | Quit at once, also while a delete runs. |

The key help wraps onto more lines at the ` · ` separators when it does not
fit the terminal width. The table shows fewer rows for each extra line.

`c` runs the first clipboard tool on `PATH`: `pbcopy`, `wl-copy`,
`xclip -selection clipboard`, or `xsel --clipboard --input`. The text goes to
stdin with no trailing newline. The copy runs outside the event loop. The
footer shows `copied <text>`, or `cannot copy: <error>`. If no tool exists,
the error is `no clipboard tool found (pbcopy, wl-copy, xclip, xsel)`. `c` does
nothing while a prune step runs.

### Bare repositories

If the main entry is bare, the tool does not show it as a row. The other
worktrees show as usual, sorted by last commit time. No row is the main
worktree. (Builder call.)

### Data for each worktree

| Value | Source |
|---|---|
| Path, branch, HEAD, locked, prunable | `git worktree list --porcelain` |
| Changed files | Line count of `git status --porcelain` in the worktree. Untracked files count. |
| Distance from base | `git rev-list --left-right --count <base>...HEAD` |
| Last commit time | `git log -1 --format=%ct` |
| Disk size | `du -sk <path>` |
| Merged into base | Read-only git commands in the worktree. See below. |

Merged into base: the tool compares `HEAD` with `<base>`. It does not do this
for the main worktree, for a missing folder, or when there is no base. The
result is one of four states: unknown, none (no mark), no commits, and merged.

1. If `HEAD` and `<base>` are the same commit, the state is none.
2. `mb = git merge-base <base> HEAD`. If `mb` is `HEAD`, `HEAD` is an
   ancestor of `<base>`. Then `c` is the first line of
   `git rev-list --first-parent --reverse HEAD..<base>`. If `c^1` is `HEAD`,
   `HEAD` is an old position of the base branch: the state is no commits. If
   not, a merge commit brought `HEAD` into the base: the state is merged. If
   the list is empty or `c` has no parent, the state is no commits.
3. If `git diff <mb> HEAD` is empty, the state is merged.
4. If `git rev-list --count --no-merges <mb>..<base>` is more than 2000, the
   state is unknown.
5. The tool calculates `git patch-id --stable` of the diff of step 3. It also
   calculates the patch ids of `git log -p --no-merges <mb>..<base>`. If the
   list has the patch id of the diff, the state is merged (a squash merge).
   If not, the state is none.

A fast-forward merge gives the state no commits, because git cannot tell it
apart from a new branch at an old commit of the base.

These commands only read, and they write no objects. `ls --json` gives the
state in two fields. `merged` is `true` only for merged. `no_commits` is
`true` only for no commits. For the other known states, both are `false`.
For unknown, both are `null`.

The list of worktrees loads first, and the table shows immediately. The other
values load in parallel, one goroutine for each worktree and value. A limit of
4 `du` processes run at one time. A cell shows `…` until its value arrives.

### Base branch

The base is the default branch of `origin`. The tool reads it from
`git symbolic-ref refs/remotes/origin/HEAD`. If that ref does not exist, the
tool tries `origin/main`, then `origin/master`. The `base` configuration value
overrides the detection.

The column header shows the base name. `↑` is the number of commits that the
worktree has and the base does not. `↓` is the number of commits that the
worktree is behind the base.

### Fetch

The user decided that the tool fetches when the screen opens.

- When the screen opens, the tool runs `git fetch origin <base branch>` one
  time, in the background. All worktrees share one repository, so one fetch
  serves all rows.
- The fetch gets only the base branch. That is faster than a full fetch.
  (Builder call.)
- The screen does not wait. The distances show immediately from the local
  copy of the remote ref, dimmed. When the fetch finishes, the tool calculates
  the distances again and shows them in the usual color.
- The fetch has a timeout of 15 seconds. If the fetch fails, the distances
  stay dimmed and the footer shows `fetch failed: distances can be stale`.
- The `f` key runs the fetch again.
- `wrktree ls` does not fetch by default, so scripts stay fast and work
  offline. `wrktree ls --fetch` fetches first and waits. (Builder call.)
- The configuration value `fetch = false` stops the fetch on open.

### Size cache

The tool stores the last size of each worktree in
`$XDG_CACHE_HOME/wrktree/sizes.json` (default `~/.cache/wrktree/`). The screen
uses this cache so that most opens of the screen run no `du` at all. Without
the cache, each start shows `…` for many seconds on a monorepo.

The user decided that an approximate size is sufficient and that a cached size
stays valid for 24 hours.

- If a cached size is less than 24 hours old, the tool shows it as the final
  value and does not run `du` for that worktree. The value has a `~` prefix,
  for example `~1.4 GB`, to show that it is approximate. (The prefix is a
  builder call.)
- If a cached size is older than 24 hours, the tool shows it dimmed with the
  `~` prefix and runs `du` in the background. The new value then replaces it.
- If a worktree has no cached size, the cell shows `…` until `du` finishes.
- The `r` key runs `du` again for all worktrees and ignores the age.
- `wrktree ls` obeys the same rules. `wrktree ls --fresh` ignores the age.
- The configuration value `size_ttl` sets the period. The default is `24h`.
- When `du` finishes for a worktree, the tool writes the new size and the
  time to the cache.
- The key is the repository path plus the worktree path.
- The tool removes the entries of worktrees that do not exist.
- The cache stores sizes only. Changed files, distances, and commit times are
  fast, so the tool reads them fresh each time.

### Switch

`wrktree init zsh` prints this function. The user adds
`eval "$(wrktree init zsh)"` to `.zshrc`.

```sh
wt() {
  local f; f="$(mktemp)"
  WRKTREE_CD_FILE="$f" command wrktree "$@"
  local rc=$?
  [ -s "$f" ] && cd "$(cat "$f")"
  rm -f "$f"
  return $rc
}
```

When the user switches, the binary writes the worktree path to the file in
`WRKTREE_CD_FILE`. The function then changes the directory. `wrktree init zsh
--name <name>` prints the function with a different name.

If `WRKTREE_CD_FILE` is not set, switch prints the path to stdout and prints a
hint about `wrktree init` to stderr. Thus `cd "$(wrktree switch 6201)"` also
works.

The screen obeys the same rule. It draws on stderr, so stdout stays free for
the path. If the user runs `wrktree` without the function and presses `enter`,
the screen quits, prints the path to stdout, and prints the hint to stderr.
(Builder call.)

### Open

The configuration value `open` is a command template. The default is
`code -n {path}`. The tool replaces `{path}` with the shell-quoted path and
runs the result with `sh -c`. The command gets the terminal of the user, so a
terminal app such as `claude` also works.

In the screen, `o` quits the screen first and then runs the command. (Builder
call. It keeps terminal apps simple.)

If the command is not found, the tool prints the command and the path of the
configuration file.

### Delete

- The main worktree cannot be deleted. The tool prints an error.
- A locked worktree cannot be deleted. The tool prints the lock reason.
- The worktree of the current directory cannot be deleted from the subcommand.
  In the screen, the tool switches the shell to the main worktree after the
  delete. If `WRKTREE_CD_FILE` is not set, or the repository has no main
  worktree, the screen refuses the delete and says why. Thus the shell never
  stays in a deleted directory.
- The user decided the screen delete keys below on 2026-11-24. The old prompt
  with the "also delete the branch" checkbox was confusing.
- In the screen, `d` deletes the worktree and keeps the branch. `D` deletes the
  worktree, then runs `git branch -d <branch>`. A branch error shows in the
  footer and does not undo the delete. For a detached worktree, `D` does the
  same as `d`. There is no checkbox and no other branch key.
- Clean worktree: in the screen, no prompt. When the check is done, the tool
  runs `git worktree remove <path>` immediately, without `--force`. Thus git
  refuses the delete if files changed after the check. Ignored files, for
  example `.env.local`, are deleted with no warning. `rm` asks one
  confirmation.
- Worktree with changed files: the prompt shows the count of changed files.
  The user must type `yes`. Then the tool runs `git worktree remove --force
  <path>`.
- Detached worktree with commits that no branch and no remote ref contains:
  the prompt shows the count of those commits. The user must type `yes`. The
  tool finds them with `git rev-list HEAD --not --branches --remotes --tags`.
- In the screen, the typed prompt shows the text of the delete prompt. If `D`
  started the delete and the worktree has a branch, the next line is
  `The branch <name> is deleted too.` Then come `Type yes to delete:` and the
  hint `type yes + enter · esc cancel`. Only `yes` and `enter` delete.
- In the screen, a delete runs in the background. The user decided this on
  2026-11-24: a delete of a large worktree froze the screen for many seconds.
  Each row that `d` or `D` started has a job, by path. The job is `checking`
  while the plan is read, then `deleting` while `git worktree remove` runs.
  Several deletes can run at the same time. Each one is its own command.
- A row with a job is dim. After its label and marks, it shows a spinner and
  the word of the job, for example `| deleting`. The spinner cycles through
  `|`, `/`, `-`, and `\` every 120 ms. A `tea.Tick` drives it, and the tick
  runs only while a row has a job. The spinner and the word are yellow. The
  cursor row keeps the cursor style. When the name is cut for the width, the
  job text stays, as the marks do.
- While rows have jobs, the list keeps all keys: moves, `/`, `c`, `f`, `r`, and
  `d` or `D` on other rows. On a row with a job, `enter`, `o`, `d`, and `D` do
  nothing and the footer shows `delete in progress`. `p` does nothing while a
  row has a job. A prune that runs still blocks the other keys.
- The plan is for the row of the `d` key, wherever the cursor is now. A clean
  plan starts the delete at once. A plan that needs `yes` moves the cursor to
  its row and opens the typed prompt, but only in the list. If the filter
  input or a different prompt is open, the delete is cancelled. A plan error,
  a blocked plan, a refused current worktree, or a row that a reload removed
  ends the job, and nothing is deleted.
- A reload keeps the jobs of the rows that stay. A row that is gone loses its
  job.
- Quit rule: while a delete runs, `q`, `esc`, `enter`, and `o` do not quit.
  The footer shows `N delete in progress: wait, or press ctrl+c` (`N deletes`
  for more than one). `ctrl+c` quits at once. If the shell is in a worktree
  that is deleted, `ctrl+c` switches the shell to the main worktree. While only
  plans run, the quit keys quit as usual.
- After the delete of the current worktree, the screen switches the shell to
  the main worktree and quits. If other deletes still run, it quits when the
  last one is done.
- `git worktree remove` also deletes ignored files, for example `.env.local`.
  The tool cannot tell these apart from `node_modules`. Thus each delete
  prompt says that ignored files are deleted too. (Builder call.)
- `rm --force` skips the prompts. `rm` without a terminal and without
  `--force` stops with an error on a worktree with changed files.
- The branch stays by default. `rm --branch` also runs `git branch -d
  <branch>`. That command refuses an unmerged branch, and the tool shows the
  git message. In the screen, `D` deletes the branch.

### Prune

The tool runs `git worktree prune --dry-run -v` and shows the entries. If
there are no entries, it says so. After confirmation, it runs `git worktree
prune -v`.

### Configuration

File: `$XDG_CONFIG_HOME/wrktree/config.toml` (default `~/.config/wrktree/`).
The file is optional.

```toml
open = "code -n {path}"   # command template
base = ""                 # empty = detect from origin
fetch = true              # fetch the base branch when the screen opens
size_ttl = "24h"          # how long a cached disk size stays valid
```

Examples for other apps: `cursor -n {path}`, `cd {path} && claude`.

### Code structure

```
main.go
internal/git/      run git, parse porcelain output, worktree operations
internal/disk/     du runner, size cache
internal/config/   load the TOML file, defaults
internal/shell/    init function text, cd-file write
internal/cmd/      cobra commands
internal/ui/       Bubble Tea model, table, prompts
```

`internal/git` and `internal/disk` have no dependency on the screen. The
subcommands and the screen call them.

Dependencies: `spf13/cobra`, `charmbracelet/bubbletea`,
`charmbracelet/lipgloss`, `BurntSushi/toml`.

### Tests

- Parsers of porcelain output: table tests with recorded output.
- Worktree operations: tests that create a temporary repository with real
  worktrees and run the real `git` binary.
- Name match, command template, and shell quote: unit tests.
- The screen: tests of the model update function with key messages. No
  terminal emulation.

## Rules for the build work

The user set these rules for the person or agent that builds the tool.

- Do not run destructive commands on the real repositories of the user. Do
  all tests of delete and prune in temporary repositories that the tests
  create.
- Do not publish anything (push, release, package registry) before the user
  says so.

## Left to the builder

- Colors and exact column widths. Long branch names truncate with `…`.
- Text of prompts and errors.
- Release tooling (GoReleaser, Homebrew) is not part of version 1.

## Go module path

`github.com/dev-shetty/wrktree`. (Builder call, from the GitHub account that
`gh` uses on this machine. The user can change it before the build starts.)
