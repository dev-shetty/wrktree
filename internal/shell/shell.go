// Package shell holds the shell integration and the open command.
package shell

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// CDFileEnv names the variable that holds the file for the new directory.
const CDFileEnv = "WRKTREE_CD_FILE"

const initTemplate = `wt() {
  local f; f="$(mktemp)"
  WRKTREE_CD_FILE="$f" command wrktree "$@"
  local rc=$?
  [ -s "$f" ] && cd "$(cat "$f")"
  rm -f "$f"
  return $rc
}
`

// InitScript returns the shell function text for shell, named name.
func InitScript(shell, name string) (string, error) {
	switch shell {
	case "zsh", "bash":
		return strings.Replace(initTemplate, "wt()", name+"()", 1), nil
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
	_, err := fmt.Fprintln(stderr, `wrktree: to change directory, add this line to your shell rc file: eval "$(wrktree init zsh)"`)
	return err
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
// stdin, stdout, and stderr of this process. If the command is not
// found, the error is an *exec.ExitError with exit code 127.
func RunOpen(template, path string) error {
	cmd := exec.Command("sh", "-c", OpenCommand(template, path))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
