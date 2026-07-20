package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard is overridable in tests.
var copyToClipboard = copyToClipboardOS

func copyToClipboardOS(text string) error {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return fmt.Errorf("nothing to copy")
	}

	switch runtime.GOOS {
	case "darwin":
		return pipeToCmd(text, "pbcopy")
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			return pipeToCmd(text, "wl-copy")
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			return pipeToCmd(text, "xclip", "-selection", "clipboard")
		}
		if _, err := exec.LookPath("xsel"); err == nil {
			return pipeToCmd(text, "xsel", "--clipboard", "--input")
		}
		return fmt.Errorf("no clipboard tool found (install wl-clipboard, xclip, or xsel)")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
}

func pipeToCmd(text string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	if _, err := stdin.Write([]byte(text)); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
