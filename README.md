# wrktree

`wrktree` shows all git worktrees of a repository in one table. You can act on a worktree with one key or one short command.

`git worktree list` prints long paths that wrap in the terminal. It does not show disk size, changed files, or the age of a worktree. `wrktree` shows these values, so you can decide quickly which worktrees to keep.

```
 wrktree · levelsfyi-mono                                  10 worktrees · 14.2 GB

   BRANCH                               CHANGES    origin/master   SIZE     LAST COMMIT
 ● master                               clean      ↑0 ↓3           3.1 GB   2 hours ago
 › eng-6201-onboarding-v2-alerts        4 files    ↑2 ↓31          1.4 GB   3 weeks ago
   eng-6458-bugsink-key-flows ✓ merged  clean      ↑1 ↓12          1.2 GB   9 days ago
   eng-6882-tp-self-serve-ui            12 files   ↑5 ↓0           …        1 day ago

 enter switch · o open · d delete · D delete + branch · p prune · f fetch · / filter · q quit
```

- `●` marks the worktree of the current directory.
- `›` marks the cursor. When the screen opens, the cursor is on the worktree of the current directory.
- The cursor row is bold and cyan. All cells of that row have this one color. No cell of that row is dim.
- `CHANGES` is the number of changed files. Untracked files count.
- `↑` is the number of commits that the worktree has and the base branch does not have.
- `↓` is the number of commits that the worktree is behind the base branch.
- `✓ merged` marks a branch that the base branch already contains. See [Merged branches](#merged-branches).
- `…` shows a value that is not available yet.
- The main worktree is always the first row. The other rows sort by the time of the last commit, newest first.

`wrktree` operates on macOS and Linux, with zsh or bash. It does not create worktrees. Use `git worktree add` to create a worktree.

## Install

You must have Go 1.24 or later.

Install with `go install`:

```sh
go install github.com/dev-shetty/wrktree@latest
```

The binary goes to `$(go env GOPATH)/bin`. Make sure that this directory is in your `PATH`.

Or build from the source:

```sh
git clone https://github.com/dev-shetty/wrktree.git
cd wrktree
go build
```

The build writes the `wrktree` binary to the current directory. Move it to a directory in your `PATH`.

## Shell setup

A program cannot change the directory of your shell. Thus `wrktree` supplies a shell function, `wt`, that changes the directory for you.

For zsh, add this line to `~/.zshrc`:

```sh
eval "$(wrktree init zsh)"
```

For bash, add this line to `~/.bashrc`:

```sh
eval "$(wrktree init bash)"
```

Then open a new shell. Use `wt` in place of `wrktree`. For example, `wt` opens the screen, and `wt switch 6201` changes the directory.

To give the function a different name, use `--name`:

```sh
eval "$(wrktree init zsh --name w)"
```

If you do not install the function, `wrktree switch` prints the path to stdout. Then you can change the directory with `cd "$(wrktree switch 6201)"`.

## The screen

Run `wt` (or `wrktree`) in a git repository. The screen opens.

If stdin or stderr is not a terminal, `wrktree` prints the table one time, as `wrktree ls` does.

| Key | Action |
|---|---|
| `↑` `↓` `←` `→` `j` `k` | Move the cursor. `←` moves up and `→` moves down. From the first row, a move up goes to the last row. |
| `enter` | Change to the worktree and quit. |
| `o` | Quit, then run the open command on the worktree. |
| `d` | Delete the worktree. The branch stays. A clean worktree is deleted immediately, with no prompt. |
| `D` | Delete the worktree, then delete its branch with `git branch -d`. |
| `p` | Show the stale worktree entries, then prune them after a prompt. |
| `f` | Fetch the base branch again, then calculate the distances again. |
| `/` | Filter the rows by branch name or folder name. |
| `r` | Read all values again, and measure all disk sizes again. |
| `q` `esc` `ctrl+c` | Quit. |

The screen draws on stderr. Stdout carries only the path for a directory change.

The git commands that only read run with `git --no-optional-locks`. Thus the background `git status` does not take `index.lock`, and a commit in a different terminal does not fail.

## Merged branches

A branch that is squash-merged keeps its own commits. Thus its `↑` count stays above 0 after the merge. The `✓ merged` mark shows that the base branch already contains the changes of the branch.

`wrktree` compares `HEAD` of each worktree with the base branch. It finds the merge base with `git merge-base`. A branch is merged if one of these conditions is true:

1. `HEAD` is an ancestor of the base branch. This is the case after a normal merge or a fast-forward merge.
2. `git diff <merge-base> HEAD` is empty. The branch has no changes of its own.
3. One commit on the base branch after the merge base has the same `git patch-id --stable` as the full diff of the branch. This is the case after a squash merge.

More rules:

- A worktree at the same commit as the base branch is not merged. It has nothing to merge.
- The main worktree never shows the mark.
- If the base branch has more than 2000 commits after the merge base, `wrktree` does not do the third test. The mark does not show.
- The mark is green in the screen. When the branch name is cut for the width, the mark stays.
- All of these git commands only read. They do not write objects to the repository.
- The answer depends on the base branch. Thus `f` and `r` calculate it again.

The third test can miss a squash merge. This occurs when the base branch changed the lines around the change before the squash commit.

## Subcommands

| Command | Action |
|---|---|
| `wrktree` | Open the screen. |
| `wrktree ls [--json] [--fetch] [--fresh]` | Print the table one time, with all values. |
| `wrktree switch <name>` | Change the shell directory to the worktree. |
| `wrktree open <name>` | Run the open command on the worktree. |
| `wrktree rm <name> [--force] [--branch]` | Delete the worktree. |
| `wrktree prune [--yes]` | Remove the entries of worktrees whose folder is missing. |
| `wrktree init <zsh\|bash> [--name <name>]` | Print the shell function. |

`<name>` is a part of the branch name or of the folder name. Case is not important.

- An exact match of the branch name or the folder name wins. Thus `master` selects `master` when `master-fix` also exists.
- If more than one worktree matches, the command prints the matches and stops with exit code 1.

`wrktree ls` flags:

- `--json` prints the rows as JSON. The `merged` field is `true` or `false`, or `null` if the answer is not known. It is `null` for the main worktree.
- `--fetch` fetches the base branch first. Without it, `ls` does not use the network.
- `--fresh` measures all disk sizes again.

`wrktree switch` and `wrktree open` refuse a worktree whose folder is missing. The error is `folder is missing: use prune`.

If the current directory is not in a git repository, each command prints `wrktree: not a git repository` and stops with exit code 1.

## Delete

CAUTION: `git worktree remove` also deletes ignored files, for example `.env.local`. `wrktree` cannot tell these files apart from `node_modules`. In the screen, `d` on a clean worktree deletes it immediately, with no prompt. The ignored files of that worktree, for example `.env.local`, are deleted with it.

These rules apply to `d` and `D` in the screen and to `wrktree rm`:

- You cannot delete the main worktree.
- You cannot delete a locked worktree. The error shows the lock reason.
- You cannot delete a worktree whose folder is missing. Use prune for it.
- You cannot delete a worktree that has a different worktree in its folder. `git worktree remove` deletes the inner worktree too, also when an ignore rule hides it. Delete the inner worktree first.
- A worktree with changed files needs the typed word `yes`. The prompt shows the number of changed files. Then `wrktree` runs `git worktree remove --force`.
- A detached worktree with commits that no branch, remote ref, or tag contains also needs the typed word `yes`. The prompt shows the number of these commits.

In the screen:

- `d` and `D` first check the worktree. While the check runs, the screen ignores all keys except the quit keys.
- If the worktree is clean, the delete starts immediately. There is no prompt. The delete does not use `--force`, so git refuses it if files changed after the check.
- If the worktree needs the typed word `yes`, a prompt opens. Type `yes` and press `enter` to delete. Any other text cancels. `esc` cancels.
- Each prompt says that ignored files are deleted too.

The branch stays by default.

- In the screen, press `D` in place of `d` to also delete the branch. The typed prompt then shows the line `The branch <name> is deleted too.`
- For a detached worktree, `D` does the same as `d`.
- With `wrktree rm`, add `--branch`.

`wrktree` deletes the branch with `git branch -d`. Git refuses to delete a branch that is not merged. In that case the worktree is deleted, the branch stays, and `wrktree` shows the git message.

The current worktree:

- `wrktree rm` does not delete a worktree whose folder holds the current directory. This rule also applies in a nested worktree inside that folder.
- The screen can delete it. After the delete, the shell changes to the main worktree. If you press `ctrl+c` while the delete runs, the shell also changes to the main worktree.
- If the `wt` function is not in use, or the repository has no main worktree, the screen refuses this delete.

`wrktree rm --force` deletes without a prompt. Without a terminal and without `--force`, `wrktree rm` stops with an error on a worktree with changed files.

If stdin closes or gives a read error before an answer, `wrktree rm` and `wrktree prune` print `cancelled` and stop with exit code 1. An answer that does not continue, for example `n`, stops with exit code 0.

## Prune

Prune removes the git entries of worktrees whose folder is missing. These rows show `[missing]` in the table.

`wrktree` runs `git worktree prune --dry-run -v` and shows the entries. After you confirm, it runs `git worktree prune -v`. In the screen, press `y` to prune. Every other key cancels. `wrktree prune --yes` prunes without a prompt.

## Configuration

The configuration file is optional. Its location is `$XDG_CONFIG_HOME/wrktree/config.toml`. If `XDG_CONFIG_HOME` is not set, the location is `~/.config/wrktree/config.toml`.

The file has 4 keys. These are the default values:

```toml
open = "code -n {path}"   # command that opens a worktree
base = ""                 # base branch. Empty = detect from origin
fetch = true              # fetch the base branch when the screen opens
size_ttl = "24h"          # how long a cached disk size stays valid
```

An unknown key is an error.

### open

`open` is a command template. `wrktree` replaces each `{path}` with the quoted path of the worktree. If the template has no `{path}`, the path goes at the end. `sh -c` runs the result. The command gets your terminal, so a terminal app also operates.

To open worktrees in Cursor:

```toml
open = "cursor -n {path}"
```

To start Claude Code in the worktree:

```toml
open = "cd {path} && claude"
```

If the command is not found, `wrktree` shows the full template and the path of the configuration file.

The command does not get the `WRKTREE_CD_FILE` variable. Thus a `wt` call inside the command cannot change the directory of the shell that started `wrktree`.

### base

The base branch is the default branch of `origin`. `wrktree` reads it from `refs/remotes/origin/HEAD`. If that ref does not exist, `wrktree` tries `origin/main`, then `origin/master`. If no base branch is found, the distance column shows `—`.

To use a different base branch, set `base`:

```toml
base = "origin/develop"
```

The remote part of `base` is the longest name of a configured remote that `base` starts with. Thus a remote or a branch name can contain a slash. A base without a remote-tracking ref, for example the local branch `main`, is not fetched. That is not an error.

### fetch

When the screen opens, `wrktree` runs `git fetch <remote> <branch>` for the base branch one time, in the background. All worktrees share one repository, so one fetch serves all rows.

- The screen does not wait for the fetch. The distances show at once from the local copy of the remote ref. They are dim until the fetch is complete.
- When the fetch is complete, `wrktree` calculates the distances again.
- The fetch stops after 15 seconds. If the fetch fails, the distances stay dim and the screen shows `fetch failed: distances can be stale`.
- `wrktree ls` does not fetch unless you add `--fetch`.
- The fetch cannot ask for a password or a passphrase on the terminal. `wrktree` sets `GIT_TERMINAL_PROMPT=0` and starts the fetch with no terminal. Your ssh command and askpass settings stay in effect. A fetch that needs a typed password fails.

To stop the fetch when the screen opens, set `fetch = false`.

### size_ttl

`du` can take many seconds on a large worktree, for example one with `node_modules`. Thus `wrktree` keeps the last size of each worktree in a cache. The cache file is `$XDG_CACHE_HOME/wrktree/sizes.json`, or `~/.cache/wrktree/sizes.json` if `XDG_CACHE_HOME` is not set.

- If a cached size is younger than `size_ttl`, `wrktree` shows it with a `~` prefix, for example `~1.4 GB`. It does not run `du` for that worktree.
- If a cached size is older than `size_ttl`, `wrktree` shows it dim with the `~` prefix. Then it runs `du` in the background and shows the new size.
- If a worktree has no cached size, the cell shows `…` until `du` is complete.
- At most 4 `du` processes run at one time.
- `r` in the screen and `wrktree ls --fresh` ignore the cache and measure all sizes again.

The cache stores sizes only. The other values are fast, so `wrktree` reads them again each time. When a worktree is deleted, its cache entry is removed.

The value of `size_ttl` is a Go duration, for example `"30m"`, `"24h"`, or `"72h"`.
