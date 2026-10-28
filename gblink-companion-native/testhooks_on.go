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
						b, err := json.Marshal(s)
						out = string(b)
						if err != nil {
							out = "error " + err.Error()
						}
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
					case "gbbank", "gbbankdrop": // the Game Boy bank: its state, or a drag ("c:0 b:3")
						s := a.ui.Snapshot()
						if s.GBBank == nil {
							out = "no bank"
							break
						}
						v := s.GBBank.view()
						if f[0] == "gbbankdrop" {
							refs := strings.Fields(arg)
							ref := func(t string) slotRef {
								k, n, _ := strings.Cut(t, ":")
								i, _ := strconv.Atoi(n)
								if k == "b" {
									return slotRef{Bank: true, Box: v.BankBox, Slot: i}
								}
								return slotRef{Box: v.CartBox, Slot: i}
							}
							if len(refs) == 2 {
								s.GBBank.Drop(ref(refs[0]), ref(refs[1]))
								a.redraw()
							}
							break
						}
						sp := func(ms [gbMaxSlots]*GBMon) []int {
							o := make([]int, gbMaxSlots)
							for i, m := range ms {
								if m != nil {
									o[i] = m.Species()
								}
							}
							return o
						}
						b, _ := json.Marshal(map[string]any{"Ready": v.Ready, "Loaded": v.CartLoaded, "Gen": v.Gen, "Game": v.GameName,
							"Note": v.Note, "ToBank": v.Counts.ToBank, "ToCart": v.Counts.ToCart, "Changed": v.Counts.Any, "Saving": v.Saving,
							"Cart": sp(v.Cart), "Bank": sp(v.BankSlots), "InBank": v.InBank})
						out = string(b)
					case "bankdrop": // a drag in the Pokémon Bank: "c:0 b:3" (side:slot of the boxes on show)
						s := a.ui.Snapshot()
						refs := strings.Fields(arg)
						if s.Bank == nil || len(refs) != 2 {
							out = "no bank"
							break
						}
						v := s.Bank.view()
						ref := func(t string) slotRef {
							k, n, _ := strings.Cut(t, ":")
							i, _ := strconv.Atoi(n)
							if k == "b" {
								return slotRef{Bank: true, Box: v.BankBox, Slot: i}
							}
							return slotRef{Box: v.CartBox, Slot: i}
						}
						s.Bank.Drop(ref(refs[0]), ref(refs[1]))
						a.redraw()
					case "bank": // the Pokémon Bank's boxes on show: species per slot (0 empty)
						s := a.ui.Snapshot()
						if s.Bank == nil {
							out = "no bank"
							break
						}
						v := s.Bank.view()
						type side struct {
							Box     int
							Species []int
						}
						r := struct {
							Ready, Saving, Changed, Loaded   bool
							SelPlace                         string // where the chosen Pokémon was met, as shown
							Mode, Note                       string
							ToBank, ToCart, Released, InBank int
							Picked                           int
							Cart, Bank                       side
							Names, Icons                     bool
							Moves, Items, Places             int
							At                               [2]float32        // the pointer, in the boxes' coordinates
							Rects                            map[string][4]int // the slots on show: "c:0" ... "b:29"
						}{Ready: v.Ready, Saving: v.Saving, Changed: v.Counts.Any, Loaded: v.CartLoaded, Mode: v.Mode, Note: v.Note, ToBank: v.Counts.ToBank,
							ToCart: v.Counts.ToCart, Released: v.Counts.Released, Picked: v.Picked,
							InBank: v.InBank, Cart: side{Box: v.CartBox}, Bank: side{Box: v.BankBox}, Names: v.Data != nil, Icons: v.Icons != nil,
							Rects: map[string][4]int{}}
						if v.SelMon != nil {
							r.SelPlace = v.Data.placeName(v.SelMon.MetPlace)
						}
						if v.Data != nil {
							r.Moves, r.Items, r.Places = len(v.Data.Moves), len(v.Data.Items), len(v.Data.Places)
						}
						if w != nil {
							done := make(chan struct{})
							w.do(func() {
								r.At = [2]float32{w.bankDrag.at.X, w.bankDrag.at.Y}
								for _, x := range w.bankDrag.rects {
									k := map[bool]string{false: "c", true: "b"}[x.ref.Bank]
									r.Rects[fmt.Sprintf("%s:%d", k, x.ref.Slot)] = [4]int{x.r.Min.X, x.r.Min.Y, x.r.Max.X, x.r.Max.Y}
								}
								close(done)
							})
							<-done
						}
						for i := range v.Cart {
							n, m := 0, 0
							if v.Cart[i] != nil {
								n = v.Cart[i].Species
							}
							if v.BankSlots[i] != nil {
								m = v.BankSlots[i].Species
							}
							r.Cart.Species = append(r.Cart.Species, n)
							r.Bank.Species = append(r.Bank.Species, m)
						}
						b, _ := json.Marshal(r)
						out = string(b)
					default:
						out = "unknown " + strconv.Quote(f[0])
					}
					fmt.Fprintln(c, out)
				}
			}(c)
		}
	}()
}
