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

// On Linux the file pickers are zenity (GNOME and most desktops) or kdialog (KDE), whichever is
// installed; without either, the window offers recent games and the games folder.
func pickFile(title, filterName, pattern, initialDir string) (string, error) {
	if initialDir == "" || initialDir == "." {
		initialDir, _ = os.UserHomeDir()
	}
	return picker([]string{"--file-selection", "--title=" + title, "--file-filter=" + filterName + " | " + pattern,
		"--filename=" + strings.TrimSuffix(initialDir, "/") + "/"},
		[]string{"--title", title, "--getopenfilename", initialDir, pattern + "|" + filterName})
}

func pickFolder(title string) (string, error) {
	home, _ := os.UserHomeDir()
	return picker([]string{"--file-selection", "--directory", "--title=" + title},
		[]string{"--title", title, "--getexistingdirectory", home})
}

func picker(zenityArgs, kdialogArgs []string) (string, error) {
	if p := os.Getenv("GBLINK_TEST_PICK"); p != "" { // tests
		return p, nil
	}
	if runtime.GOOS == "darwin" {
		return "", fmt.Errorf("file picker not available here")
	}
	for _, tool := range []struct {
		name string
		args []string
	}{{"zenity", zenityArgs}, {"kdialog", kdialogArgs}} {
		if _, err := exec.LookPath(tool.name); err != nil {
			continue
		}
		out, err := exec.Command(tool.name, tool.args...).Output()
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok { // cancelled
				return "", nil
			}
			continue
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", fmt.Errorf("no file picker available: install zenity (or kdialog on KDE)")
}

func showInFolder(path string) { exec.Command("xdg-open", path).Start() }

func browserCandidates() []string {
	return []string{"/usr/bin/microsoft-edge", "/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
}

func openDefault(url string) { exec.Command("xdg-open", url).Start() }

func fatal(msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(1) }

// osDarkMode follows the desktop's dark style on Linux (GNOME, and KDE/others through the GTK theme).
func osDarkMode() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output(); err == nil &&
		strings.Contains(string(out), "dark") {
		return true
	}
	if out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "gtk-theme").Output(); err == nil &&
		strings.Contains(strings.ToLower(string(out)), "dark") {
		return true
	}
	return strings.Contains(strings.ToLower(os.Getenv("GTK_THEME")), "dark")
}

func pickSaveFile(title, dir, name string) (string, error) {
	if p := os.Getenv("GBLINK_TEST_SAVEAS"); p != "" { // tests
		return p, nil
	}
	return picker([]string{"--file-selection", "--save", "--confirm-overwrite", "--title=" + title, "--filename=" + filepath.Join(dir, name)},
		[]string{"--title", title, "--getsavefilename", filepath.Join(dir, name)})
}
