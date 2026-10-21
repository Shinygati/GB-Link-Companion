//go:build testhooks

// Test build only (go build -tags testhooks): a local control port so the end-to-end test can
// press the same buttons the player would, and read the state.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

func startTestHooks(a *App) {
	if l := os.Getenv("GBLINK_TEST_LANG"); l != "" { // the GBA's text in this language, whatever the game
		testLang = l[0]
	}
	testFind = os.Getenv("GBLINK_TEST_FIND") != ""
	port := os.Getenv("GBLINK_TEST_HOOKS")
	if port == "" {
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewScanner(c)
				for r.Scan() {
					f := strings.SplitN(r.Text(), " ", 2)
					arg := ""
					if len(f) > 1 {
						arg = f[1]
					}
					out := "ok"
					a.winMu.Lock()
					w := a.win
					a.winMu.Unlock()
					switch f[0] {
					case "state":
						s := a.ui.Snapshot()
						b, _ := json.Marshal(s)
						out = string(b)
					case "click": // a button by id
						if w != nil {
							w.do(func() { w.b(arg).Click() })
						}
					case "game": // click a game row (twice = double-click)
						if w != nil {
							w.do(func() {
								if cl := w.games[arg]; cl != nil {
									cl.Click()
								}
							})
						}
					case "answer": // a prompt button
						s := a.ui.Snapshot()
						if w != nil && s.Prompt != nil {
							id := s.Prompt.ID
							w.do(func() { w.promptBtn(id, arg).Click() })
						} else {
							out = "no prompt"
						}
					case "search":
						if w != nil {
							w.do(func() { w.search.SetText(arg) })
						}
					case "selected":
						if w != nil {
							out = w.selected
						}
					case "settings":
						var m map[string]any
						json.Unmarshal([]byte(arg), &m)
						a.setSettings(func(s *Settings) {
							if v, ok := m["mgba"].(string); ok {
								s.MGBA = v
							}
						})
					case "raise":
						a.raise()
					default:
						out = "unknown " + strconv.Quote(f[0])
					}
					fmt.Fprintln(c, out)
				}
			}(c)
		}
	}()
}
