// GB-Link Companion: play GBA games in mGBA with the save on your real cartridge.
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"
)

const instancePort = 47188 // only one copy runs; a second one hands over to it

func main() {
	rom := ""
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-") {
			if abs, err := filepath.Abs(a); err == nil {
				rom = abs
			}
		}
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", instancePort))
	if err != nil && os.Getenv("GBLINK_MULTI") == "" {
		// already running: ask it to show its window (and add the ROM), then quit
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", instancePort), time.Second); err == nil {
			fmt.Fprintf(c, "show\n%s\n", rom)
			c.Close()
			return
		}
	}

	a := &App{set: loadSettings()}
	if a.set.MGBA == "" {
		a.set.MGBA = DefaultMGBA()
	} else if _, err := os.Stat(a.set.MGBA); err != nil {
		if d := DefaultMGBA(); d != "" {
			a.set.MGBA = d
		}
	}
	if rom != "" {
		a.set.addRecent(rom)
	}
	a.set.save()
	a.ui = newUI(a)
	a.ui.st.Settings = a.set
	a.ui.st.MGBAFound = a.set.MGBA != ""
	a.refreshLibrary()
	if os.Getenv("GBLINK_TEST_HOOKS") == "" {
		go a.checkUpdates(false) // a newer release on GitHub?
	}

	if ln != nil {
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					defer c.Close()
					c.SetDeadline(time.Now().Add(2 * time.Second))
					r := bufio.NewReader(c)
					cmd, _ := r.ReadString('\n')
					path, _ := r.ReadString('\n')
					if strings.TrimSpace(cmd) == "show" {
						if p := strings.TrimSpace(path); p != "" && !a.busy() {
							a.addRecent(p)
							a.selectGame(p)
						}
						a.raise()
					}
				}()
			}
		}()
	}
	startTestHooks(a)

	// the adapter indicator on the game list
	go func() {
		for {
			if !a.busy() {
				port := os.Getenv("GBLINK_PORT")
				if port == "" {
					port = FindGBLinkPort()
				}
				a.mu.Lock()
				a.mu.Unlock()
				cur := a.ui.Snapshot().Adapter
				if cur != port {
					a.ui.update(func(s *State) { s.Adapter = port })
				}
			}
			time.Sleep(2 * time.Second)
		}
	}()

	a.openWindow()
	if rom != "" {
		a.selectGame(rom)
	}
	app.Main()
}
