// The window's Wonder Cards: a card on the play screen, and the list to pick one from.
package main

import (
	"fmt"
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// wcClicks handles the Wonder Card buttons (called from handleClicks).
func (w *Window) wcClicks(gtx C) {
	a := w.app
	if w.b("wc-open").Clicked(gtx) {
		w.wcOpen = true
		a.ui.update(func(st *State) { st.WCStatus = "" })
	}
	if w.b("wc-close").Clicked(gtx) || w.b("wc-scrim").Clicked(gtx) {
		w.wcOpen = false
	}
	cards := wonderCardsFor(w.st.WCGame)
	chosen := map[string]bool{}
	for _, l := range w.st.WCChosen {
		chosen[l] = true
	}
	changed := false
	for _, c := range cards {
		if w.b("wc-tick:" + c.File).Clicked(gtx) {
			chosen[c.Label] = !chosen[c.Label]
			if chosen[c.Label] && c.Resident { // only one card that keeps running at a time
				for _, o := range cards {
					if o.Resident && o.Label != c.Label {
						delete(chosen, o.Label)
					}
				}
			}
			changed = true
		}
		if w.b("wc-row:" + c.File).Clicked(gtx) {
			if w.wcSel == c.File {
				w.wcSel = ""
			} else {
				w.wcSel = c.File
			}
		}
	}
	var picked []WonderCard
	var labels []string
	for _, c := range cards { // in the list's order
		if chosen[c.Label] {
			picked = append(picked, c)
			labels = append(labels, c.Label)
		}
	}
	if changed {
		w.st.WCChosen = labels
		go a.chooseWonderCards(labels)
	}
	if w.b("wc-replace").Clicked(gtx) && w.st.WCReplace != nil {
		c := *w.st.WCReplace
		go a.replaceWonderCard(c)
	}
	if w.b("wc-use").Clicked(gtx) && len(picked) > 0 {
		a.ui.update(func(st *State) { st.WCStatus, st.WCReplace = "Sending…", nil })
		go a.useWonderCards(picked)
	}
	if w.b("wc-add").Clicked(gtx) && len(picked) == 1 && !wcJapanese(w.st.WCGame) {
		c := picked[0]
		a.ui.update(func(st *State) { st.WCStatus = "Sending " + c.Label + "…" })
		go a.addWonderCard(c)
	}
}

// wcCard: the Wonder Cards card on the play screen.
func (w *Window) wcCard(gtx C) D {
	st := w.st
	text, can := "", false
	switch {
	case st.WCGame == "" && st.WCWhy == "":
		text = "Checking the ROM…"
	case st.WCGame == "":
		text = st.WCWhy
	case !st.Controller.ScriptConnected:
		text = "Waiting for mGBA's script (it sends the cards to the game)."
	default:
		text, can = "Send any of the GB-Link team's Wonder Cards to the game while you play: add one for the deliveryman to hand over, or use one straight away.", true
		if wcJapanese(st.WCGame) {
			text = "Send any of the GB-Link team's Wonder Cards to the game while you play: their gifts or effects happen straight away."
		}
	}
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		return w.card(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, "Wonder Cards") }),
						layout.Rigid(gap(4)),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 13.5, font.Normal, w.pal.muted, text) }))
				}),
				layout.Rigid(gap(16)),
				layout.Rigid(func(gtx C) D {
					if !can {
						return D{}
					}
					return w.button(gtx, w.b("wc-open"), "primary", "Wonder Cards…", false, nil)
				}))
		})
	})
}

// wcDialog: the cards for this game, grouped; the chosen one opens up with its description
// and "Use now" / "Receive Wondercard".
func (w *Window) wcDialog(gtx C) {
	w.b("wc-scrim").Layout(gtx, func(gtx C) D {
		fill(gtx, gtx.Constraints.Max, 0, w.pal.scrim)
		return D{Size: gtx.Constraints.Max}
	})
	st := w.st
	cards := wonderCardsFor(st.WCGame)
	type row struct {
		group string
		card  *WonderCard
	}
	var rows []row
	last := ""
	for i := range cards {
		if cards[i].Group != last {
			last = cards[i].Group
			rows = append(rows, row{group: last})
		}
		rows = append(rows, row{card: &cards[i]})
	}
	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(32), gtx.Dp(640))
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Max.Y -= gtx.Dp(48)
		dialog := func(gtx C) D {
			return w.box(gtx, w.pal.surface, w.pal.border, 16, 20, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D { return w.label(gtx, 19, font.SemiBold, w.pal.text, "Wonder Cards") }),
							layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("wc-close"), "secondary", "Done", false, nil) }))
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 6, Bottom: 8}.Layout(gtx, func(gtx C) D {
							return w.label(gtx, 13, font.Normal, w.pal.muted,
								"Use now: tick one or more cards and their gifts or effects happen straight away, one after another, and the card your game has stays. "+
									"Cards marked “until reset” keep running until the game is reset; only one of them can run at a time, so ticking one unticks the other, and using one while another is on offers to replace it. "+
									"Your ticks are remembered for this game. "+
									"Receive Wondercard: the card is saved like a Mystery Gift card, for the deliveryman on any Pokémon Center's 2F (save in the game to keep it). "+
									"Cards from github.com/GB-Link/gblink-wondercards (GPL-3.0), made for your game and its language."+japaneseNote(st.WCGame))
						})
					}),
					layout.Rigid(func(gtx C) D {
						if st.WCStatus == "" {
							return D{}
						}
						return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
							if st.WCReplace == nil {
								return w.label(gtx, 14, font.SemiBold, w.pal.accent, st.WCStatus)
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx C) D { return w.label(gtx, 14, font.SemiBold, w.pal.accent, st.WCStatus) }),
								layout.Rigid(gap(10)),
								layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("wc-replace"), "primary", "Replace", false, nil) }))
						})
					}),
					layout.Flexed(1, func(gtx C) D {
						return material.List(w.th, &w.wcList).Layout(gtx, len(rows), func(gtx C, i int) D {
							r := rows[i]
							if r.card == nil {
								return layout.Inset{Top: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
									return w.label(gtx, 13, font.SemiBold, w.pal.muted, r.group)
								})
							}
							c := r.card
							open := w.wcSel == c.File
							ticked := false
							for _, l := range st.WCChosen {
								if l == c.Label {
									ticked = true
								}
							}
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									bg := w.pal.surface
									if open {
										bg = w.pal.surface2
									}
									return w.box(gtx, bg, mix(w.pal.border, bg, .5), 8, 8, func(gtx C) D {
										gtx.Constraints.Min.X = gtx.Constraints.Max.X
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx C) D { // the tick box
												return w.b("wc-tick:"+c.File).Layout(gtx, func(gtx C) D {
													sz := gtx.Dp(20)
													if ticked {
														fill(gtx, image.Pt(sz, sz), gtx.Dp(5), w.pal.accent)
														g := gtx
														g.Constraints = layout.Exact(image.Pt(sz, sz))
														return layout.Center.Layout(g, func(gtx C) D {
															return w.label(gtx, 13, font.Bold, w.pal.bg, "✓")
														})
													}
													fill(gtx, image.Pt(sz, sz), gtx.Dp(5), w.pal.border)
													in := gtx.Dp(2)
													st := op.Offset(image.Pt(in, in)).Push(gtx.Ops)
													fill(gtx, image.Pt(sz-2*in, sz-2*in), gtx.Dp(4), bg)
													st.Pop()
													return D{Size: image.Pt(sz, sz)}
												})
											}),
											layout.Rigid(gap(10)),
											layout.Flexed(1, func(gtx C) D { // the name: shows what it does
												return w.b("wc-row:"+c.File).Layout(gtx, func(gtx C) D {
													gtx.Constraints.Min.X = gtx.Constraints.Max.X
													name := c.Label
													if c.Resident {
														name += "  · until reset"
													}
													return w.label(gtx, 14.5, font.Normal, w.pal.text, name)
												})
											}))
									})
								}),
								layout.Rigid(func(gtx C) D {
									if !open {
										return D{}
									}
									return layout.Inset{Left: 40, Right: 10, Top: 6, Bottom: 8}.Layout(gtx, func(gtx C) D {
										return w.label(gtx, 13.5, font.Normal, w.pal.text, c.Description)
									})
								}),
								layout.Rigid(gap(4)))
						})
					}),
					layout.Rigid(func(gtx C) D {
						n := len(st.WCChosen)
						return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									if n == 0 {
										return w.label(gtx, 13.5, font.Normal, w.pal.muted, "Tick the cards you want. Click a name to see what it does.")
									}
									t := "Use now"
									if n > 1 {
										t = fmt.Sprintf("Use all %d now", n)
									}
									return w.button(gtx, w.b("wc-use"), "primary", t, false, nil)
								}),
								layout.Rigid(gap(10)),
								layout.Rigid(func(gtx C) D {
									if n != 1 || wcJapanese(st.WCGame) { // the Japanese games: use now only
										return D{}
									}
									return w.button(gtx, w.b("wc-add"), "secondary", "Receive Wondercard", false, nil)
								}))
						})
					}))
			})
		}
		return w.b("wc-dialog-catch").Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
			return dialog(gtx)
		})
	})
}

func japaneseNote(key string) string {
	if wcJapanese(key) {
		return " The Japanese games can only use a card now: Receive Wondercard isn't offered for them."
	}
	return ""
}
