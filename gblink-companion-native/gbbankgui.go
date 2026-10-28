// The Game Boy Pokémon Bank's window: the game's boxes and the bank's side by side (20 slots,
// 5 across, as the games have them), and the chosen Pokémon's summary. Moving Pokémon is always a
// drag; a click shows a Pokémon's summary (or chooses it in Move Multi and Release).
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

const gbCols = 5

func (w *Window) gbBankClicks(gtx C) {
	a := w.app
	for _, id := range []string{"gbbank", "gbbank-again"} {
		if w.b(id).Clicked(gtx) {
			w.settingsOpen, w.setPage, w.toolsPage = false, "", ""
			go func() {
				if err := a.openGBBank(); err != nil {
					a.ui.Toast(err.Error())
				}
			}()
		}
	}
	if w.b("gbbank-folder").Clicked(gtx) {
		recoverGBBankFolders()
		os.MkdirAll(gbBankBoxesDir(), 0o755)
		openDefault(gbBankBoxesDir())
	}
	b := w.st.GBBank
	if b == nil || w.st.Mode != "gbbank" {
		return
	}
	for _, c := range []string{"save", "undo", "done", "release", "load", "file"} {
		if w.b("gbbank-" + c).Clicked(gtx) {
			go a.gbBankSend(c)
		}
	}
	for _, m := range []string{modeMove, modeMulti, modeRelease} {
		if w.b("gbbank-mode:" + m).Clicked(gtx) {
			b.SetMode(m)
		}
	}
	if w.b("gbbank-clear").Clicked(gtx) {
		b.ClearPicks()
	}
	for _, side := range []bool{false, true} {
		k := map[bool]string{false: "c", true: "b"}[side]
		if w.b("gbbank-prev:" + k).Clicked(gtx) {
			b.Turn(side, -1)
		}
		if w.b("gbbank-next:" + k).Clicked(gtx) {
			b.Turn(side, 1)
		}
		if w.b("gbbank-all:" + k).Clicked(gtx) {
			b.SelectBox(side)
		}
	}
	for _, side := range []bool{false, true} { // the slots as buttons too, for the tests
		k := map[bool]string{false: "c", true: "b"}[side]
		for i := 0; i < gbMaxSlots; i++ {
			if w.b(fmt.Sprintf("gbbank-slot:%s:%d", k, i)).Clicked(gtx) {
				v := b.view()
				b.Tap(slotRef{Bank: side, Box: v.shownBox(side), Slot: i})
			}
		}
	}
	if w.b("gbbank-add").Clicked(gtx) {
		b.AddBox()
	}
}

func (v gbBankView) shownBox(bank bool) int {
	if bank {
		return v.BankBox
	}
	return v.CartBox
}

func (v *gbBankView) has(r slotRef) bool {
	if r.Box != v.shownBox(r.Bank) {
		return false
	}
	if r.Bank {
		return v.BankSlots[r.Slot] != nil
	}
	return v.Cart[r.Slot] != nil
}

func (v *gbBankView) isPicked(r slotRef) bool {
	if r.Box != v.shownBox(r.Bank) {
		return false
	}
	if r.Bank {
		return v.BankPick[r.Slot]
	}
	return v.CartPick[r.Slot]
}

func (w *Window) gbBankPointer(gtx C, b *GBBankSession, v *gbBankView) {
	d := &w.gbDrag
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: d, Kinds: pointer.Press | pointer.Drag | pointer.Release |
			pointer.Move | pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		d.at = e.Position
		switch e.Kind {
		case pointer.Enter, pointer.Move:
			d.over = true
		case pointer.Leave:
			d.over = false
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary {
				continue
			}
			d.press, d.dragging, d.boxing, d.pressed, d.pressAt = nil, false, false, true, e.Position
			d.pressMod = e.Modifiers.Contain(key.ModCtrl) || e.Modifiers.Contain(key.ModShift) || e.Modifiers.Contain(key.ModCommand)
			if r, ok := d.slotAt(e.Position); ok {
				d.press = &r
			}
		case pointer.Drag:
			if !d.pressed || d.dragging || d.boxing || v.Saving {
				continue
			}
			if dx, dy := e.Position.X-d.pressAt.X, e.Position.Y-d.pressAt.Y; dx*dx+dy*dy <= float32(gtx.Dp(6)*gtx.Dp(6)) {
				continue
			}
			onMon := d.press != nil && v.has(*d.press)
			switch {
			case v.Mode == modeMove:
				d.dragging = onMon
			case v.Mode == modeMulti && onMon && v.isPicked(*d.press):
				d.dragging = true
			default:
				d.boxing = true
			}
		case pointer.Release:
			to, ok := d.slotAt(e.Position)
			switch {
			case d.boxing:
				b.SelectRefs(d.inBox(), d.pressMod)
			case d.press == nil:
			case d.dragging && ok:
				b.Drop(*d.press, to)
			case !d.dragging && ok && to == *d.press:
				b.Tap(to)
			}
			d.press, d.dragging, d.boxing, d.pressed = nil, false, false, false
		case pointer.Cancel:
			d.press, d.dragging, d.boxing, d.pressed = nil, false, false, false
		}
	}
}

func (w *Window) gbBankEditor(gtx C) D {
	b := w.st.GBBank
	if b == nil {
		return D{}
	}
	v := b.view()
	if !v.Ready {
		return D{}
	}
	w.gbBankPointer(gtx, b, &v)
	d := &w.gbDrag
	d.rects = d.rects[:0]
	m := op.Record(gtx.Ops)
	W := gtx.Constraints.Max.X
	bar := w.gbBankBar(gtx, &v)
	y := bar.Size.Y + gtx.Dp(14)
	sp := gtx.Dp(14)
	wide := W >= gtx.Dp(1060)
	sumW, gaps := 0, 1
	if wide {
		sumW, gaps = gtx.Dp(340), 2
	}
	panelW := (W - sumW - sp*gaps) / 2
	h := 0
	for _, bank := range []bool{true, false} {
		x := 0
		if bank {
			x = panelW + sp
		}
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min.X, g.Constraints.Max.X, g.Constraints.Min.Y = panelW, panelW, 0
		var pd D
		if !bank && !v.CartLoaded {
			g.Constraints.Min.Y = h
			pd = w.gbBankNoCart(g, &v)
		} else {
			pd = w.gbBankPanel(g, &v, bank, image.Pt(x, y))
		}
		st.Pop()
		h = max(h, pd.Size.Y)
	}
	if wide {
		st := op.Offset(image.Pt(2*(panelW+sp), y)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min.X, g.Constraints.Max.X, g.Constraints.Min.Y = sumW, sumW, 0
		sd := w.gbBankSummary(g, &v)
		st.Pop()
		h = max(h, sd.Size.Y)
	}
	y += h
	if !wide && v.SelMon != nil {
		y += sp
		st := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min.X, g.Constraints.Min.Y = W, 0
		sd := w.gbBankSummary(g, &v)
		st.Pop()
		y += sd.Size.Y
	}
	call := m.Stop()
	size := image.Pt(W, y)
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, d)
	if d.dragging {
		pointer.CursorGrabbing.Add(gtx.Ops)
	} else if r, ok := d.slotAt(d.at); d.over && ok && v.has(r) && !v.Saving {
		pointer.CursorPointer.Add(gtx.Ops)
	}
	call.Add(gtx.Ops)
	if d.dragging && d.press != nil { // the Pokémon in the hand follows the pointer
		p := v.Cart[d.press.Slot]
		if d.press.Bank {
			p = v.BankSlots[d.press.Slot]
		}
		if p != nil && d.press.Box == v.shownBox(d.press.Bank) {
			s := gtx.Dp(56)
			st := op.Offset(image.Pt(int(d.at.X)-s/2, int(d.at.Y)-s/2)).Push(gtx.Ops)
			w.gbMonTile(gtx, p, s, w.pal.accentSoft)
			if n := v.Picked; v.Mode == modeMulti && v.isPicked(*d.press) && n > 1 {
				o := op.Offset(image.Pt(s-gtx.Dp(14), 0)).Push(gtx.Ops)
				bd := gtx.Dp(22)
				fill(gtx, image.Pt(bd, bd), bd/2, w.pal.accent)
				g := gtx
				g.Constraints = layout.Exact(image.Pt(bd, bd))
				layout.Center.Layout(g, func(gtx C) D { return w.label(gtx, 11.5, font.Bold, color.NRGBA{255, 255, 255, 255}, itoa(n)) })
				o.Pop()
			}
			st.Pop()
		}
	}
	if d.boxing {
		r, a, bw := d.box(), w.pal.accent, gtx.Dp(1)
		edge := func(x0, y0, x1, y1 int, c color.NRGBA) {
			st := op.Offset(image.Pt(x0, y0)).Push(gtx.Ops)
			fill(gtx, image.Pt(x1-x0, y1-y0), 0, c)
			st.Pop()
		}
		edge(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y, color.NRGBA{R: a.R, G: a.G, B: a.B, A: 40})
		edge(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+bw, a)
		edge(r.Min.X, r.Max.Y-bw, r.Max.X, r.Max.Y, a)
		edge(r.Min.X, r.Min.Y, r.Min.X+bw, r.Max.Y, a)
		edge(r.Max.X-bw, r.Min.Y, r.Max.X, r.Max.Y, a)
	}
	area.Pop()
	return D{Size: size}
}

// gbMonTile: a Pokémon in a slot (the Game Boy games have no box icons: its name).
func (w *Window) gbMonTile(gtx C, p *GBMon, s int, bg color.NRGBA) {
	fill(gtx, image.Pt(s, s), gtx.Dp(8), bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	name := gbSpeciesName(p.Species())
	if p.Egg() {
		name = "Egg"
	}
	layout.Center.Layout(g, func(gtx C) D {
		return layout.UniformInset(4).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 11, font.SemiBold, w.pal.text, name, 2) }),
				layout.Rigid(func(gtx C) D {
					if p.Egg() {
						return D{}
					}
					return w.label(gtx, 10, font.Normal, w.pal.muted, fmt.Sprintf("Lv %d", p.Level()))
				}))
		})
	})
}

func (w *Window) gbBankBar(gtx C, v *gbBankView) D {
	return w.card(gtx, func(gtx C) D {
		hint := map[string]string{
			modeMove: "Drag a Pokémon to another slot. One already there swaps places. Click one to see its summary.",
			modeMulti: "Drag a box over the slots to choose every Pokémon in it (hold Ctrl to add more), or click them one by one. " +
				"Then drag one of the chosen ones to where they should go.",
			modeRelease: "Click the Pokémon to release, or drag a box over them, then press Release. Once you save they're gone for good.",
		}[v.Mode]
		if !v.CartLoaded && v.Mode != modeRelease {
			hint += " Open a game's boxes to move Pokémon to and from it."
		}
		tone := w.pal.muted
		switch {
		case v.Saving:
			hint, tone = "Saving…", w.pal.text
		case v.Note != "":
			hint, tone = v.Note, w.pal.text
		}
		c := v.Counts
		summary, stone := "No changes yet.", w.pal.muted
		if c.Any {
			var parts []string
			if c.ToBank > 0 {
				parts = append(parts, fmt.Sprintf("%d to the bank", c.ToBank))
			}
			if c.ToCart > 0 {
				parts = append(parts, fmt.Sprintf("%d to the game", c.ToCart))
			}
			if c.Released > 0 {
				parts = append(parts, fmt.Sprintf("%d released", c.Released))
			}
			summary, stone = "Not saved yet", w.pal.warn
			if len(parts) > 0 {
				summary += ": " + strings.Join(parts, ", ")
			}
			summary += "."
		}
		modeBtn := func(m, label string) layout.FlexChild {
			return layout.Rigid(func(gtx C) D {
				style := "secondary"
				if v.Mode == m {
					style = "primary"
				}
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b("gbbank-mode:"+m), style, label, false, nil) })
			})
		}
		btn := func(id, style, label string) layout.FlexChild {
			return layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b(id), style, label, false, nil) })
			})
		}
		var actions []layout.FlexChild
		if n := v.Picked; n > 0 && !v.Saving {
			switch v.Mode {
			case modeMulti:
				actions = append(actions,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 14, font.SemiBold, w.pal.accent, fmt.Sprintf("%d chosen", n)) }),
					btn("gbbank-clear", "ghost", "Clear"))
			case modeRelease:
				actions = append(actions,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 14, font.SemiBold, w.pal.bad, fmt.Sprintf("%d marked", n)) }),
					btn("gbbank-release", "primary", fmt.Sprintf("Release %d", n)), btn("gbbank-clear", "ghost", "Clear"))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				kids := []layout.FlexChild{modeBtn(modeMove, "Move"), modeBtn(modeMulti, "Move Multi"), modeBtn(modeRelease, "Release"),
					layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} })}
				if c.Any && !v.Saving {
					kids = append(kids, btn("gbbank-undo", "ghost", "Undo changes"), layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b("gbbank-save"), "primary", "Save", false, icCheck) })
					}))
				}
				if !v.Saving {
					kids = append(kids, btn("gbbank-done", "secondary", "Done"))
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, kids...)
			}),
			layout.Rigid(gap(12)),
			layout.Rigid(func(gtx C) D { return w.label(gtx, 13.5, font.Normal, tone, hint) }),
			layout.Rigid(gap(8)),
			layout.Rigid(func(gtx C) D {
				kids := []layout.FlexChild{layout.Flexed(1, func(gtx C) D { return w.label(gtx, 14, font.SemiBold, stone, summary) })}
				kids = append(kids, actions...)
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, kids...)
			}))
	})
}

func (w *Window) gbBankPanel(gtx C, v *gbBankView, bank bool, base image.Point) D {
	k := map[bool]string{false: "c", true: "b"}[bank]
	title := v.GameName
	sub := fmt.Sprintf("%s · ID %05d", v.Trainer, v.TID)
	if v.File != "" {
		sub += " · save file"
	}
	name, count := v.CartName, fmt.Sprintf("Box %d of %d", v.CartBox+1, v.CartBoxes)
	slots, picks := v.Cart, v.CartPick
	if bank {
		title, sub = "Game Boy Pokémon Bank", fmt.Sprintf("On this PC · %d Pokémon", v.InBank)
		name, count = v.BankName, fmt.Sprintf("Box %d of %d", v.BankBox+1, v.BankBoxes)
		slots, picks = v.BankSlots, v.BankPick
	}
	pad := gtx.Dp(22)
	return w.card(gtx, func(gtx C) D {
		head := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.Y = gtx.Dp(48)
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 16, font.SemiBold, w.pal.text, title, 1) }),
							layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 12.5, font.Normal, w.pal.muted, sub, 1) }))
					}),
					layout.Rigid(func(gtx C) D {
						if v.Mode == modeMove || v.Saving {
							return D{}
						}
						return w.button(gtx, w.b("gbbank-all:"+k), "ghost", "Select all", false, nil)
					}),
					layout.Rigid(func(gtx C) D {
						if !bank {
							return D{}
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								if v.BankBox != v.BankBoxes-1 || v.BankBoxes >= gbBankMax {
									return D{}
								}
								return w.button(gtx, w.b("gbbank-add"), "ghost", "+ Box", false, nil)
							}),
							layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("gbbank-folder"), "ghost", "Folder", false, icFolder) }))
					}))
			}),
			layout.Rigid(gap(10)),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("gbbank-prev:"+k), "secondary", "‹", false, nil) }),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.centered(gtx, 15, font.SemiBold, w.pal.text, name) }),
							layout.Rigid(func(gtx C) D { return w.centered(gtx, 12, font.Normal, w.pal.muted, count) }))
					}),
					layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("gbbank-next:"+k), "secondary", "›", false, nil) }))
			}),
			layout.Rigid(gap(10)))
		gy := head.Size.Y
		st := op.Offset(image.Pt(0, gy)).Push(gtx.Ops)
		grid := w.gbBankGrid(gtx, v, bank, slots, picks, base.Add(image.Pt(pad, pad+gy)))
		st.Pop()
		y := gy + grid.Size.Y
		foot := ""
		switch {
		case !bank && v.PartyCount > 0:
			foot = fmt.Sprintf("Your party (%d) isn't shown: put a Pokémon in a box in the game to move it.", v.PartyCount)
		case bank:
			n := 0
			for _, p := range v.BankSlots {
				if p != nil {
					n++
				}
			}
			foot = fmt.Sprintf("%d of %d slots used in this box.", n, gbBoxSlots)
		}
		st = op.Offset(image.Pt(0, y+gtx.Dp(10))).Push(gtx.Ops)
		w.labelMax(gtx, 12, font.Normal, w.pal.muted, foot, 1)
		st.Pop()
		y += gtx.Dp(10) + gtx.Dp(18)
		return D{Size: image.Pt(gtx.Constraints.Max.X, y)}
	})
}

func (w *Window) gbBankNoCart(gtx C, v *gbBankView) D {
	minY := gtx.Constraints.Min.Y
	return w.box(gtx, w.pal.surface, w.pal.border, 14, 22, func(gtx C) D {
		gtx.Constraints.Min.Y = max(0, minY-2*gtx.Dp(22))
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return icon(gtx, icGamepad, w.pal.muted, 44) }),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D { return w.centered(gtx, 17, font.SemiBold, w.pal.text, "Your game") }),
				layout.Rigid(gap(6)),
				layout.Rigid(func(gtx C) D {
					return w.centered(gtx, 13.5, font.Normal, w.pal.muted, "Look through the bank as it is, or open the boxes of Pokémon Red, Blue, Yellow, Gold, Silver "+
						"or Crystal: from the cartridge over GB-Link, or from a save file (a flash cart's SD card or an emulator).")
				}),
				layout.Rigid(gap(16)),
				layout.Rigid(func(gtx C) D {
					if v.Loading {
						return w.spinner(gtx, 24)
					}
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return w.button(gtx, w.b("gbbank-load"), "primary", "Load from cartridge", false, icDownload)
						}),
						layout.Rigid(gap(10)),
						layout.Rigid(func(gtx C) D {
							return w.button(gtx, w.b("gbbank-file"), "secondary", "Open a save file…", false, icFolder)
						}))
				}))
		})
	})
}

func (w *Window) gbBankGrid(gtx C, v *gbBankView, bank bool, slots [gbMaxSlots]*GBMon, picks [gbMaxSlots]bool, origin image.Point) D {
	n := gbBoxSlots
	if !bank && v.CartSlots > 0 {
		n = v.CartSlots // 30 in a Japanese game's box
	}
	sp := gtx.Dp(6)
	size := min((gtx.Constraints.Max.X-(gbCols-1)*sp)/gbCols, gtx.Dp(72))
	left := (gtx.Constraints.Max.X - gbCols*size - (gbCols-1)*sp) / 2
	box := v.shownBox(bank)
	d := &w.gbDrag
	var target *slotRef
	if d.dragging {
		if r, ok := d.slotAt(d.at); ok {
			target = &r
		}
	}
	inBox := map[slotRef]bool{}
	if d.boxing {
		for _, r := range d.inBox() {
			inBox[r] = true
		}
	}
	rows := (n + gbCols - 1) / gbCols
	for i := 0; i < n; i++ {
		x, y := left+i%gbCols*(size+sp), i/gbCols*(size+sp)
		here := slotRef{Bank: bank, Box: box, Slot: i}
		d.rects = append(d.rects, bankRect{here, image.Rectangle{Min: origin.Add(image.Pt(x, y)), Max: origin.Add(image.Pt(x+size, y+size))}})
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(size, size))
		w.gbBankSlot(g, v, slots[i], here, picks[i] || inBox[here] && slots[i] != nil, target != nil && *target == here)
		st.Pop()
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, rows*size+(rows-1)*sp)}
}

func (w *Window) gbBankSlot(gtx C, v *gbBankView, p *GBMon, here slotRef, picked, target bool) {
	d := &w.gbDrag
	size := gtx.Constraints.Min
	sel := v.Sel != nil && *v.Sel == here
	hover := !d.dragging && d.over && !v.Saving
	if hover {
		r, ok := d.slotAt(d.at)
		hover = ok && r == here
	}
	moving := d.dragging && d.press != nil && (*d.press == here || v.Mode == modeMulti && picked && v.isPicked(*d.press))
	bg, border := w.pal.surface2, color.NRGBA{}
	switch {
	case target:
		bg, border = w.pal.accentSoft, w.pal.accent
	case picked && v.Mode == modeRelease:
		bg, border = w.pal.badSoft, w.pal.bad
	case picked:
		bg, border = w.pal.accentSoft, w.pal.accent
	case sel:
		border = w.pal.accent
	case hover:
		border = mix(w.pal.accent, w.pal.border, .45)
	}
	r := gtx.Dp(8)
	if border.A > 0 {
		fill(gtx, size, r, border)
		bw := gtx.Dp(2)
		o := op.Offset(image.Pt(bw, bw)).Push(gtx.Ops)
		fill(gtx, size.Sub(image.Pt(2*bw, 2*bw)), max(r-bw, 0), bg)
		o.Pop()
	} else {
		fill(gtx, size, r, bg)
	}
	if p == nil {
		return
	}
	g := gtx
	layout.Center.Layout(g, func(gtx C) D {
		return layout.UniformInset(4).Layout(gtx, func(gtx C) D {
			name := gbSpeciesName(p.Species())
			if p.Egg() {
				name = "Egg"
			}
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 11, font.SemiBold, w.pal.text, name, 2) }),
				layout.Rigid(func(gtx C) D {
					if p.Egg() {
						return D{}
					}
					return w.label(gtx, 10, font.Normal, w.pal.muted, fmt.Sprintf("Lv %d · G%d", p.Level(), p.Gen)+map[string]string{gbJapanese: " · JP", gbKorean: " · KO"}[p.Region()])
				}))
		})
	})
	mark := func(t string, c color.NRGBA, right bool) {
		x := gtx.Dp(4)
		if right {
			x = size.X - gtx.Dp(15)
		}
		o := op.Offset(image.Pt(x, gtx.Dp(1))).Push(gtx.Ops)
		w.label(gtx, 12, font.Bold, c, t)
		o.Pop()
	}
	switch {
	case !p.Valid():
		mark("!", w.pal.bad, true)
	case p.Shiny() && !p.Egg():
		mark("★", w.pal.warn, true)
	}
	if picked {
		if v.Mode == modeRelease {
			mark("✕", w.pal.bad, false)
		} else {
			mark("✓", w.pal.accent, false)
		}
	}
	if p.HeldItem() != 0 && !p.Egg() {
		dot := gtx.Dp(7)
		o := op.Offset(image.Pt(size.X-dot-gtx.Dp(4), size.Y-dot-gtx.Dp(4))).Push(gtx.Ops)
		fill(gtx, image.Pt(dot, dot), dot/2, w.pal.warn)
		o.Pop()
	}
	if moving {
		fill(gtx, size, r, color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 150})
	}
}

func (w *Window) gbBankSummary(gtx C, v *gbBankView) D {
	return w.card(gtx, func(gtx C) D {
		p := v.SelMon
		if p == nil {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, "Summary") }),
				layout.Rigid(gap(6)),
				layout.Rigid(func(gtx C) D {
					return w.label(gtx, 13.5, font.Normal, w.pal.muted, "Click a Pokémon to see everything about it here: its DVs, stat experience, moves, held item and more.")
				}))
		}
		var rows []layout.FlexChild
		add := func(wd layout.Widget) { rows = append(rows, layout.Rigid(wd)) }
		kv := func(k, val string) {
			if val == "" {
				return
			}
			add(func(gtx C) D {
				return layout.Inset{Top: 3}.Layout(gtx, func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Dp(110)
							return w.label(gtx, 13, font.Normal, w.pal.muted, k)
						}),
						layout.Flexed(1, func(gtx C) D { return w.label(gtx, 13, font.Normal, w.pal.text, val) }))
				})
			})
		}
		section := func(t string) {
			add(func(gtx C) D {
				return layout.Inset{Top: 12, Bottom: 2}.Layout(gtx, func(gtx C) D {
					return w.label(gtx, 11.5, font.Bold, w.pal.accent, strings.ToUpper(t))
				})
			})
		}
		species := gbSpeciesName(p.Species())
		nick := p.Nickname()
		head, sub := nick, species
		if p.Egg() {
			head, sub = "Egg", species
		} else if nick == "" || strings.EqualFold(nick, species) {
			head, sub = species, ""
		}
		if g := p.Gender(); g != "" && !p.Egg() {
			head += map[string]string{"M": " ♂", "F": " ♀"}[g]
		}
		add(func(gtx C) D {
			line2 := nonEmpty(sub)
			if !p.Egg() {
				line2 = append(line2, fmt.Sprintf("Lv %d", p.Level()))
			}
			line2 = append(line2, fmt.Sprintf("Generation %d data", p.Gen))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 17, font.SemiBold, w.pal.text, head, 1) }),
				layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 13.5, font.Normal, w.pal.muted, strings.Join(line2, " · "), 1) }),
				layout.Rigid(func(gtx C) D {
					if !p.Shiny() || p.Egg() {
						return D{}
					}
					return w.label(gtx, 13, font.SemiBold, w.pal.warn, "★ Shiny (in Gold, Silver and Crystal)")
				}))
		})
		section("About")
		kv("OT", p.OT())
		kv("ID", fmt.Sprintf("%05d", p.TID()))
		if p.Gen == 2 {
			item := gbItemName(p.HeldItem())
			if item == "" {
				item = "None"
			}
			kv("Held item", item)
			if p.Egg() {
				kv("Egg cycles", itoa(p.Friendship()))
			} else {
				kv("Friendship", itoa(p.Friendship()))
			}
			switch pk := p.Pokerus(); {
			case pk == 0:
				kv("Pokérus", "Never had it")
			case pk&15 == 0:
				kv("Pokérus", "Cured")
			default:
				kv("Pokérus", "Infected")
			}
			if cd := p.CaughtData(); cd != 0 {
				kv("Caught at level", itoa(cd>>8&0x3F))
			}
		} else {
			kv("Catch rate", fmt.Sprintf("%d (becomes %s in Gold, Silver and Crystal)", p.CatchRate(), nonBlank(gbItemName(int(itemFuture1(byte(p.CatchRate())))), "no item")))
		}
		kv("Experience", itoa(p.EXP()))
		section("DVs and stat experience")
		dv, se := p.DVs(), p.StatExp()
		names := []string{"HP", "Attack", "Defense", "Speed", "Special"}
		for i, n := range names {
			kv(n, fmt.Sprintf("DV %d · stat exp %d", dv[i], se[i]))
		}
		section("Moves")
		some := false
		for i, m := range p.Moves() {
			if m == 0 {
				continue
			}
			some = true
			pp := fmt.Sprintf("PP %d", p.PP(i))
			if u := p.PPUps(i); u > 0 {
				pp += fmt.Sprintf(" (PP Up ×%d)", u)
			}
			kv(gbMoveName(m), pp)
		}
		if !some {
			add(func(gtx C) D { return w.label(gtx, 13, font.Normal, w.pal.muted, "No moves") })
		}
		if m := v.SelMeta; m != nil && m.Added != "" {
			kv("In the bank", "since "+m.Added)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	})
}

var _ = unit.Dp(0)
