package shell

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"/a/b":     "'/a/b'",
		"/my repo": "'/my repo'",
		"/it's":    `'/it'\''s'`,
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenCommand(t *testing.T) {
	cases := []struct{ tmpl, path, want string }{
		{"code -n {path}", "/my repo", "code -n '/my repo'"},
		{"cd {path} && claude", "/a", "cd '/a' && claude"},
		{"echo {path} {path}", "/a", "echo '/a' '/a'"},
		{"code -n", "/my repo", "code -n '/my repo'"},
	}
	for _, c := range cases {
		if got := OpenCommand(c.tmpl, c.path); got != c.want {
			t.Errorf("OpenCommand(%q, %q) = %q, want %q", c.tmpl, c.path, got, c.want)
		}
	}
}

func TestRunOpenPathWithSpaceAndQuote(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "it's a dir")
	out := filepath.Join(t.TempDir(), "out")
	if err := RunOpen(`printf %s {path} > `+Quote(out), dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestRunOpenNotFound(t *testing.T) {
	err := RunOpen("wrktree-no-such-command-xyz {path}", "/a")
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 127 {
		t.Errorf("err = %v, want ExitError with code 127", err)
	}
}

func TestWriteCDFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cd")
	t.Setenv(CDFileEnv, f)
	var so, se bytes.Buffer
	if err := WriteCD("/my repo", &so, &se); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "/my repo" {
		t.Errorf("file = %q, want path with no newline", got)
	}
	if so.Len() != 0 || se.Len() != 0 {
		t.Errorf("want no output, got stdout %q stderr %q", so.String(), se.String())
	}
}

func TestWriteCDStdout(t *testing.T) {
	t.Setenv(CDFileEnv, "")
	os.Unsetenv(CDFileEnv)
	var so, se bytes.Buffer
	if err := WriteCD("/a", &so, &se); err != nil {
		t.Fatal(err)
	}
	if so.String() != "/a\n" {
		t.Errorf("stdout = %q", so.String())
	}
	want := `wrktree: to change directory, add this line to your shell rc file: eval "$(wrktree init zsh)"`
	if !strings.Contains(se.String(), want) {
		t.Errorf("stderr = %q, want hint %q", se.String(), want)
	}
}

func TestInitScript(t *testing.T) {
	got, err := InitScript("zsh", "wt")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"wt() {", `WRKTREE_CD_FILE="$f" command wrktree "$@"`} {
		if !strings.Contains(got, s) {
			t.Errorf("script lacks %q", s)
		}
	}
	if b, err := InitScript("bash", "w"); err != nil || !strings.Contains(b, "w() {") {
		t.Errorf("bash script: err %v, text %q", err, b)
	}
	if _, err := InitScript("fish", "wt"); err == nil {
		t.Error("fish: want error")
	}
	for _, sh := range []string{"zsh", "bash"} {
		bin, err := exec.LookPath(sh)
		if err != nil {
			t.Logf("skip %s -n: not installed", sh)
			continue
		}
		text, _ := InitScript(sh, "wt")
		cmd := exec.Command(bin, "-n")
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s -n: %v\n%s", sh, err, out)
		}
	}
}

func TestInitScriptBadName(t *testing.T) {
	for _, name := range []string{"", "1wt", "w t", "wt;rm", "w-t", "wt()", "$(x)"} {
		if _, err := InitScript("zsh", name); err == nil {
			t.Errorf("name %q: want error", name)
		}
	}
	for _, name := range []string{"wt", "_w", "W9_x"} {
		if _, err := InitScript("bash", name); err != nil {
			t.Errorf("name %q: %v", name, err)
		}
	}
}
