// Package shell holds the shell integration and the open command.
package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// CDFileEnv names the variable that holds the file for the new directory.
const CDFileEnv = "WRKTREE_CD_FILE"

// initTemplate defines the function with the name of the program. In a
// shell, a function has priority over a program, so wrktree also changes the
// directory. `command wrktree` runs the program, not the function.
const initTemplate = `wrktree() {
  local f; f="$(mktemp)"
  WRKTREE_CD_FILE="$f" command wrktree "$@"
  local rc=$?
  [ -s "$f" ] && cd "$(cat "$f")"
  rm -f "$f"
  return $rc
}
`

var funcName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// InitScript returns the shell function text for shell: the function
// wrktree and a short function, named name, that calls it.
// The name must match ^[A-Za-z_][A-Za-z0-9_]*$.
func InitScript(shell, name string) (string, error) {
	if !funcName.MatchString(name) {
		return "", fmt.Errorf("invalid function name %q: use letters, digits, and underscores", name)
	}
	switch shell {
	case "zsh", "bash":
		if name == "wrktree" {
			return initTemplate, nil
		}
		return initTemplate + name + "() {\n  wrktree \"$@\"\n}\n", nil
	}
	return "", fmt.Errorf("unsupported shell %q: use zsh or bash", shell)
}

// WriteCD tells the shell function to change to path. With CDFileEnv set,
// it writes path to that file. If not, it prints path and a hint.
func WriteCD(path string, stdout, stderr io.Writer) error {
	if f := os.Getenv(CDFileEnv); f != "" {
		return os.WriteFile(f, []byte(path), 0o600)
	}
	if _, err := fmt.Fprintln(stdout, path); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stderr, cdHint(os.Getenv("SHELL")))
	return err
}

// cdHint returns the setup instruction for the login shell of the user.
func cdHint(loginShell string) string {
	sh, rc := "zsh", "~/.zshrc"
	if filepath.Base(loginShell) == "bash" {
		sh, rc = "bash", "~/.bashrc"
	}
	return fmt.Sprintf(`wrktree: cannot change the directory of your shell. Add this line to %s, then open a new shell: eval "$(wrktree init %s)"`, rc, sh)
}

// Quote returns s in POSIX single-quote form.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// OpenCommand replaces every {path} in template with the quoted path.
// A template without {path} gets the quoted path at the end.
func OpenCommand(template, path string) string {
	if !strings.Contains(template, "{path}") {
		return template + " " + Quote(path)
	}
	return strings.ReplaceAll(template, "{path}", Quote(path))
}

// RunOpen runs the open command with `sh -c`. The command uses the
// stdin, stdout, and stderr of this process. It does not get CDFileEnv,
// so a wt call inside it cannot write the file of this call. If the
// command is not found, the error is an *exec.ExitError with exit code 127.
func RunOpen(template, path string) error {
	cmd := exec.Command("sh", "-c", OpenCommand(template, path))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k != CDFileEnv {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	return cmd.Run()
}

// clipboardTools are the clipboard tools that Copy tries, in order.
var clipboardTools = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
}

// Copy puts text on the system clipboard. It runs the first tool of
// clipboardTools that is on PATH, with text on stdin and no trailing newline.
// If a tool fails, Copy tries the next one: wl-copy fails in an X11 session,
// where xclip can operate. If all tools fail, the error is that of the last
// one. The tool gets no stdout or stderr pipe: xclip and xsel stay in the
// background to serve the clipboard, and a pipe would make Copy wait for them.
func Copy(text string) error {
	var failed error
	for _, tool := range clipboardTools {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			failed = fmt.Errorf("%s: %w", tool[0], err)
			continue
		}
		return nil
	}
	if failed != nil {
		return failed
	}
	return errors.New("no clipboard tool found (pbcopy, wl-copy, xclip, xsel)")
}
