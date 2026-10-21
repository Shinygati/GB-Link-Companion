//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// zenity (Linux) gives a file picker; without it, the window offers recent games and the games folder.
func pickFile(title, filterName, pattern, initialDir string) (string, error) {
	return zenity("--file-selection", "--title="+title, "--file-filter="+filterName+" | "+pattern)
}

func pickFolder(title string) (string, error) {
	return zenity("--file-selection", "--directory", "--title="+title)
}

func zenity(args ...string) (string, error) {
	if p := os.Getenv("GBLINK_TEST_PICK"); p != "" { // tests
		return p, nil
	}
	if runtime.GOOS == "darwin" {
		return "", fmt.Errorf("file picker not available here")
	}
	out, err := exec.Command("zenity", args...).Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", fmt.Errorf("no file picker available (install zenity)")
	}
	return strings.TrimSpace(string(out)), nil
}

func showInFolder(path string) { exec.Command("xdg-open", path).Start() }

func browserCandidates() []string {
	return []string{"/usr/bin/microsoft-edge", "/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
}

func openDefault(url string) { exec.Command("xdg-open", url).Start() }

func fatal(msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(1) }

func osDarkMode() bool { return false }

func pickSaveFile(title, dir, name string) (string, error) {
	if p := os.Getenv("GBLINK_TEST_SAVEAS"); p != "" { // tests
		return p, nil
	}
	return zenity("--file-selection", "--save", "--confirm-overwrite", "--title="+title, "--filename="+filepath.Join(dir, name))
}
