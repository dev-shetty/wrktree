package cmd

import (
	"os/exec"
	"testing"
)

// exec127 returns the error of a shell command that exits with code 127.
func exec127(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 127").Run()
	if err == nil {
		t.Fatal("want exit error")
	}
	return err
}
