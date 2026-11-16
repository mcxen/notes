package frontend

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunnableCodeRefusesWhatCannotRun(t *testing.T) {
	for name, code := range map[string]string{
		"empty":    "",
		"blank":    " \n\t\n",
		"too long": strings.Repeat("echo hi\n", maxRunCode/8+1),
		"a NUL":    "echo a\x00b",
	} {
		if got, err := runnableCode(code); err == nil {
			t.Errorf("%s: runs as %q", name, got)
		}
	}
	got, err := runnableCode("echo a\r\necho b\recho c")
	if err != nil || got != "echo a\necho b\necho c" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRunPreviewSaysWhatItLeavesOut(t *testing.T) {
	if got, more := runPreview("echo hi\n"); got != "echo hi" || more != 0 {
		t.Fatalf("short: %q, %d", got, more)
	}
	long := strings.Repeat("x", runPreviewWidth+5)
	got, more := runPreview(long + "\n" + strings.Repeat("echo\n", runPreviewLines+2))
	lines := strings.Split(got, "\n")
	if len(lines) != runPreviewLines || more != 3 {
		t.Fatalf("%d lines shown, %d more", len(lines), more)
	}
	if lines[0] != strings.Repeat("x", runPreviewWidth)+"…" {
		t.Fatalf("first line = %q", lines[0])
	}
}

// The script is what the terminal is handed: it has to be the user's alone,
// runnable, run the code in the home folder, and be gone once it has run.
func TestRunScriptRunsTheCodeAndRemovesItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	home := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HOME", home)

	path, err := writeRunScript("pwd -P > out.txt\necho \"$1\" >> out.txt")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 || filepath.Ext(path) != ".command" {
		t.Fatalf("%s has mode %v", path, info.Mode().Perm())
	}
	if out, err := exec.Command(path, "arg").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the script is still there: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(home)
	if string(got) != want+"\narg\n" {
		t.Fatalf("out.txt = %q, want the home folder %q and the argument", got, want)
	}
}
