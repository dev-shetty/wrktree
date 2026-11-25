# wrktree

[![CI](https://github.com/dev-shetty/wrktree/actions/workflows/ci.yml/badge.svg)](https://github.com/dev-shetty/wrktree/actions/workflows/ci.yml)

See all git worktrees of a repository in one table. Switch, sync, or delete a worktree with one key.

```
 wrktree · myapp                                            4 worktrees · 6.9 GB

   BRANCH                     CHANGES    origin/main   SIZE     LAST COMMIT
 ● main                       clean      ↑0 ↓3         3.1 GB   2 hours ago
 › fix-login-redirect         4 files    ↑2 ↓31        1.4 GB   3 weeks ago
   add-export-csv ✓ merged    clean      ↑1 ↓12        1.2 GB   9 days ago
   upgrade-deps               12 files   ↑5 ↓0         1.2 GB   1 day ago

 enter switch · o open · c copy · s sync · d delete · D delete folder + branch · f fetch · / filter · q quit
```

`git worktree list` does not show disk size, changed files, or the age of a worktree. `wrktree` shows them, and it marks the branches that are already merged. Then you know which worktrees you can delete.

`wrktree` runs on macOS and Linux, with zsh or bash. It does not create worktrees. Use `git worktree add` for that.

## Install

Use the install script. It downloads the newest release for your system and checks the checksum:

```sh
curl -sSL https://raw.githubusercontent.com/dev-shetty/wrktree/main/install.sh | sh
```

The script installs to `/usr/local/bin` if you can write to it, and to `~/.local/bin` if you cannot. To select a different folder, set `WRKTREE_INSTALL_DIR`.

If you have Go 1.24 or later, you can also build from source:

```sh
go install github.com/dev-shetty/wrktree@latest
```

You can also download an archive from the [releases page](https://github.com/dev-shetty/wrktree/releases). The macOS binaries are signed and notarized by Apple.

To see the installed version, run `wrktree --version`.

Then add the shell function to `~/.zshrc` or `~/.bashrc`:

```sh
eval "$(wrktree init zsh)"    # or: wrktree init bash
```

Open a new shell. The line adds the short command `wt`. `wt` and `wrktree` do the same thing. To use a different short name, add `--name`, for example `wrktree init zsh --name w`.

The line is optional, but `enter` needs it. A program cannot change the directory of your shell. Thus `wrktree` writes the path to a file, and the shell function does the `cd`.

Without the line, all other keys and commands operate. `enter` and `wrktree switch` only print the path, so you must type `cd "$(wrktree switch <name>)"`.

## Use

Run `wt` in a git repository.

| Key | Action |
|---|---|
| `↑` `↓` `j` `k` | Move the cursor. |
| `enter` | Change to the worktree. |
| `o` | Open the worktree in your editor. |
| `c` | Copy the branch name. |
| `s` | Merge the base branch into the worktree. |
| `d` | Delete the worktree. The branch stays. |
| `D` | Delete the worktree and its branch. |
| `f` | Fetch the base branch again. |
| `r` | Read all values again. |
| `/` | Filter by branch name or folder name. |
| `q` `esc` | Quit. |

The columns:

- `●` is the worktree of the current directory. `›` is the cursor.
- `CHANGES` is the number of changed files. Untracked files count.
- `↑` and `↓` are the commits ahead of and behind the base branch.
- `✓ merged` shows that the base branch already contains the branch. Squash merges count.
- `no commits` shows that the branch has no commits of its own.
- `[missing]` shows that the folder is gone. Press `d` to remove the record.
- `~` before a size shows that the size comes from the cache.

## Commands

Each key also has a command, for scripts and for quick use.

| Command | Action |
|---|---|
| `wt ls [--json] [--fetch] [--fresh]` | Print the table one time. |
| `wt switch <name>` | Change to the worktree. |
| `wt open <name>` | Open the worktree in your editor. |
| `wt sync <name>` | Merge the base branch into the worktree. |
| `wt rm <name> [--branch] [--force]` | Delete the worktree. |
| `wt prune [--yes]` | Remove the records of worktrees whose folder is gone. |

`<name>` is a part of the branch name or the folder name. `wt switch login` finds `fix-login-redirect`. If more than one worktree matches, the command prints the matches and stops.

## Delete

CAUTION: A delete also removes the ignored files of the worktree, for example `.env.local`.

- A clean worktree is deleted immediately, with no prompt.
- A worktree with changed files needs the typed word `yes`.
- A worktree with an initialized submodule also needs the typed word `yes`.
- You cannot delete the main worktree or a locked worktree.
- `D` deletes the branch with `git branch -d`. Git keeps a branch that is not merged.
- If you delete the worktree that your shell is in, the shell changes to the main worktree.

## Sync

A sync fetches the base branch, then runs `git merge --no-edit <base>` in the worktree.

- A sync refuses a worktree with changed files. Commit or stash first.
- If the merge has a conflict, `wrktree` cancels it with `git merge --abort`. The worktree stays as it was.
- A sync never pushes, rebases, resets, or stashes.

## Configuration

The configuration file is optional. Its location is `~/.config/wrktree/config.toml`. These are the default values:

```toml
open = "code -n {path}"   # command for the o key. See below
base = ""                 # base branch. Empty = the default branch of origin
fetch = true              # fetch the base branch when the screen opens
size_ttl = "24h"          # how long a cached disk size stays valid
```

If the file does not set `open`, `wrktree` uses `$VISUAL`, then `$EDITOR`, then `code -n {path}`.

Examples for `open`:

```toml
open = "cursor -n {path}"
open = "cd {path} && claude"
```

## License

MIT
