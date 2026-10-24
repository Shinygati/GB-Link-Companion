package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// screenshotFolder: where "Save screenshot" puts its pictures (Pictures > GB-Link Companion).
func screenshotFolder() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Pictures", "GB-Link Companion")
}

// saveScreenshot saves a picture of what the GBA's status screen shows right now (the copy kept
// in gbascreen.go) and says so; the window's "Save screenshot" button.
func (a *App) saveScreenshot() {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil || s.helper == nil || s.bridge == nil {
		a.ui.Toast("Start the game first.")
		return
	}
	path, err := s.saveScreenshot()
	if err != nil {
		a.ui.Toast("No screenshot: " + err.Error())
		return
	}
	a.ui.SetController(func(c *ControllerView) { c.LastShot = path })
	a.ui.Toast("Screenshot saved: " + filepath.Base(path))
}

func (s *Session) saveScreenshot() (string, error) {
	dir := screenshotFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	title, code := "GBA game", ""
	if s.game != nil {
		title, code = s.game.Title, s.game.Code
	}
	name := strings.TrimSuffix(fileName(title, code), ".gba")
	screen := modeKeys[s.bridge.currentMode()]
	if screen == "" || screen == "controller" {
		return "", errors.New("the GBA isn't showing a status screen. Hold SELECT on it and pick one.")
	}
	path := uniquePath(filepath.Join(dir, name+" - "+screen+" - "+time.Now().Format("2006-01-02 15-04-05")+".png"))
	if err := s.helper.shot.savePNG(path); err != nil {
		return "", err
	}
	return path, nil
}
