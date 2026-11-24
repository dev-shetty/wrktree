package shell

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

// fakeTool writes a tool into dir that saves its arguments and its stdin.
func fakeTool(t *testing.T, dir, name, out string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + Quote(out+".args") + "\n/bin/cat > " + Quote(out) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCopy(t *testing.T) {
	cases := []struct {
		tools []string
		used  string
		args  string
	}{
		{[]string{"pbcopy", "wl-copy", "xclip", "xsel"}, "pbcopy", ""},
		{[]string{"wl-copy", "xclip", "xsel"}, "wl-copy", ""},
		{[]string{"xclip", "xsel"}, "xclip", "-selection clipboard"},
		{[]string{"xsel"}, "xsel", "--clipboard --input"},
	}
	for _, c := range cases {
		dir, outDir := t.TempDir(), t.TempDir()
		for _, tool := range c.tools {
			fakeTool(t, dir, tool, filepath.Join(outDir, tool))
		}
		t.Setenv("PATH", dir)
		if err := Copy("feat-a"); err != nil {
			t.Fatalf("%s: %v", c.used, err)
		}
		got, err := os.ReadFile(filepath.Join(outDir, c.used))
		if err != nil || string(got) != "feat-a" {
			t.Errorf("%s: stdin %q, err %v", c.used, got, err)
		}
		args, _ := os.ReadFile(filepath.Join(outDir, c.used+".args"))
		if strings.TrimSpace(string(args)) != c.args {
			t.Errorf("%s: args %q, want %q", c.used, args, c.args)
		}
		for _, other := range c.tools[1:] {
			if _, err := os.Stat(filepath.Join(outDir, other)); err == nil {
				t.Errorf("%s ran after %s", other, c.used)
			}
		}
	}
}
