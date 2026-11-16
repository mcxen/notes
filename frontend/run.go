package frontend

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

// A code block's commands, run in the system's terminal.
//
// The page asks; the answer is the user's, given to a dialog of the system's
// that shows what would run. The page cannot answer it, so a script that got
// into a page (a paste, a synced note, an assistant's edit) can ask but
// cannot run anything by itself.

// maxRunCode is the longest code a block may run: far more than a note's
// commands are, and little enough to write to a file without a thought.
const maxRunCode = 64 << 10

// The dialog shows this much of the code, and says how much it left out.
const (
	runPreviewLines = 20
	runPreviewWidth = 160
)

var (
	errRunUnsupported = errors.New("running code is only available on macOS")
	errRunEmpty       = errors.New("there is nothing to run")
	errRunTooLong     = errors.New("the code is too long to run")
)

// RunInTerminal runs a Bash code block's text in the system's terminal,
// after the user agrees to it in a dialog that shows the text. A dialog
// that is dismissed runs nothing, and is no error.
func (n *Notebooks) RunInTerminal(ctx context.Context, code string) error {
	if runtime.GOOS != "darwin" {
		return errRunUnsupported
	}
	code, err := runnableCode(code)
	if err != nil {
		return err
	}
	preview, more := runPreview(code)
	if more > 0 {
		preview += "\n" + tr("… and %d more lines", more)
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  mygo.CallerWindow(ctx),
		Type:    mygo.MessageWarning,
		Message: tr("Run this in Terminal?"),
		Detail:  preview + "\n\n" + tr("It runs as you, and can change or delete anything you can. Run only what you understand."),
		Buttons: []string{tr("Run"), tr("Cancel")},
		// Enter cancels: a dialog that comes up while typing runs nothing.
		DefaultButton: 1,
		CancelButton:  1,
	})
	if err != nil {
		return err
	}
	if res.Button != 0 {
		return nil
	}
	path, err := writeRunScript(code)
	if err != nil {
		return err
	}
	// A .command file opens in the terminal, which runs it in a window of its
	// own: the user's own shell environment, a prompt for a password, and the
	// output there to read.
	if err := mygo.Shell.OpenPath(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// runnableCode is the code as it will run: line ends as Unix has them, and
// refused when it is empty, too long, or holds what no script does.
func runnableCode(code string) (string, error) {
	code = strings.ReplaceAll(code, "\r\n", "\n")
	code = strings.ReplaceAll(code, "\r", "\n")
	if strings.TrimSpace(code) == "" {
		return "", errRunEmpty
	}
	if len(code) > maxRunCode || strings.ContainsRune(code, 0) {
		return "", errRunTooLong
	}
	return code, nil
}

// runPreview is the code as the dialog shows it: its first lines, long ones
// cut short, and how many lines were left out.
func runPreview(code string) (preview string, more int) {
	lines := strings.Split(strings.TrimRight(code, "\n"), "\n")
	if len(lines) > runPreviewLines {
		more = len(lines) - runPreviewLines
		lines = lines[:runPreviewLines]
	}
	for i, line := range lines {
		if r := []rune(line); len(r) > runPreviewWidth {
			lines[i] = string(r[:runPreviewWidth]) + "…"
		}
	}
	return strings.Join(lines, "\n"), more
}

// runScript is the file the terminal runs. It removes itself first (the
// shell has it open, and reads on), then starts in the home folder, as a new
// terminal window does.
func runScript(code string) string {
	if !strings.HasSuffix(code, "\n") {
		code += "\n"
	}
	return "#!/usr/bin/env bash\nrm -f -- \"$0\"\ncd \"$HOME\" || exit\n" + code
}

// writeRunScript writes the script where only this user can read it, as a
// file the terminal may run, and returns its path.
func writeRunScript(code string) (string, error) {
	f, err := os.CreateTemp("", "notes-run-*.command")
	if err != nil {
		return "", err
	}
	path := f.Name()
	_, err = f.WriteString(runScript(code))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(path, 0o700)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
