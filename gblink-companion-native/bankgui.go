// The Pokémon Bank's window: the cartridge's boxes and the bank's side by side with their box
// icons, and the chosen Pokémon's summary beside them, as PKHeX lays them out. At the top, the
// three modes (Move, Move Multi, Release); Pokémon can be dragged from slot to slot in all of them.
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// bankIconCache: the icons as pictures for the window, made once each.
type bankIconCache struct {
	src *bankIcons
	ops map[int]paint.ImageOp
}

func (c *bankIconCache) get(ic *bankIcons, i int) (paint.ImageOp, bool) {
	if ic == nil {
		return paint.ImageOp{}, false
	}
	if c.src != ic {
		c.src, c.ops = ic, map[int]paint.ImageOp{}
	}
	if op, ok := c.ops[i]; ok {
		return op, true
	}
	img := ic.image(i)
	if img == nil {
		return paint.ImageOp{}, false
	}
	op := paint.NewImageOp(img)
	op.Filter = paint.FilterNearest
	c.ops[i] = op
	return op, true
}

// bankDrag: the pointer over the boxes (a press, and a drag once it moves far enough).
type bankDrag struct {
	press    *slotRef // the slot the button went down on
	pressAt  f32.Point
	pressMod bool       // Ctrl or Shift was held
	pressed  bool       // the button is down over the boxes
	dragging bool       // moving a Pokémon (or the chosen ones)
	boxing   bool       // drawing a box to choose everything in it (Move Multi, Release)
	at       f32.Point  // where the pointer is (in the boxes' own coordinates)
	over     bool       // the pointer is over the boxes
	rects    []bankRect // where each slot was drawn last frame
}

type bankRect struct {
	ref slotRef
	r   image.Rectangle
}

// box: the box being drawn, from where the button went down to the pointer.
func (d *bankDrag) box() image.Rectangle {
	return image.Rectangle{Min: image.Pt(int(d.pressAt.X), int(d.pressAt.Y)), Max: image.Pt(int(d.at.X), int(d.at.Y))}.Canon()
}

// inBox: the slots the box being drawn touches.
func (d *bankDrag) inBox() []slotRef {
	var out []slotRef
	sel := d.box()
	for _, r := range d.rects {
		if r.r.Overlaps(sel) {
			out = append(out, r.ref)
		}
	}
	return out
}

func (d *bankDrag) slotAt(p f32.Point) (slotRef, bool) {
	pt := image.Pt(int(p.X), int(p.Y))
	for _, r := range d.rects {
		if pt.In(r.r) {
			return r.ref, true
		}
	}
	return slotRef{}, false
}

// bankClicks handles the bank's buttons (called from handleClicks).
func (w *Window) bankClicks(gtx C) {
	a := w.app
	for _, id := range []string{"bank", "bank-again", "bank-gba"} {
		if w.b(id).Clicked(gtx) {
			w.settingsOpen, w.setPage, w.toolsPage = false, "", ""
			go func() {
				if err := a.openBank(); err != nil {
					a.ui.Toast(err.Error())
				}
			}()
		}
	}
	if w.b("bank-folder").Clicked(gtx) || w.b("bank-folder-set").Clicked(gtx) {
		recoverBankFolders()
		os.MkdirAll(bankBoxesDir(), 0o755)
		openDefault(bankBoxesDir())
	}
	if w.b("bank-backups").Clicked(gtx) {
		os.MkdirAll(bankBackupsDir(), 0o755)
		openDefault(bankBackupsDir())
	}
	b := w.st.Bank
	if b == nil || w.st.Mode != "bank" {
		return
	}
	for _, c := range []string{"save", "undo", "done", "release", "load", "file"} {
		if w.b("bank-" + c).Clicked(gtx) {
			go a.bankSend(c)
		}
	}
	for _, m := range []string{modeMove, modeMulti, modeRelease} {
		if w.b("bank-mode:" + m).Clicked(gtx) {
			b.SetMode(m)
		}
	}
	if w.b("bank-to-bank").Clicked(gtx) {
		b.SendTo(true)
	}
	if w.b("bank-to-cart").Clicked(gtx) {
		b.SendTo(false)
	}
	if w.b("bank-clear").Clicked(gtx) {
		b.ClearPicks()
	}
	for _, side := range []bool{false, true} {
		k := map[bool]string{false: "c", true: "b"}[side]
		if w.b("bank-prev:" + k).Clicked(gtx) {
			b.Turn(side, -1)
		}
		if w.b("bank-next:" + k).Clicked(gtx) {
			b.Turn(side, 1)
		}
		if w.b("bank-all:" + k).Clicked(gtx) {
			b.SelectBox(side)
		}
		// the slots as buttons too, for the tests (a real click or drag goes through bankPointer)
		for i := 0; i < g3BoxSlots; i++ {
			if w.b(fmt.Sprintf("bank-slot:%s:%d", k, i)).Clicked(gtx) {
				b.Tap(slotRef{Bank: side, Box: b.view().shownBox(side), Slot: i})
			}
		}
	}
	if w.b("bank-add").Clicked(gtx) {
		b.AddBox()
	}
}

func (v bankView) shownBox(bank bool) int {
	if bank {
		return v.BankBox
	}
	return v.CartBox
}

// bankPointer: presses, drags and drops over the boxes (positions from last frame's layout).
func (w *Window) bankPointer(gtx C, b *BankSession, v *bankView) {
	d := &w.bankDrag
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
			onMon := d.press != nil && w.bankHas(v, *d.press)
			switch {
			case v.Mode == modeMove:
				d.dragging = onMon
			case v.Mode == modeMulti && onMon && v.isPicked(*d.press):
				d.dragging = true // the chosen ones move together
			default:
				d.boxing = true // Move Multi or Release: a box chooses everything in it
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

// bankHas: a Pokémon is in that slot (of the boxes on show).
func (w *Window) bankHas(v *bankView, r slotRef) bool {
	if r.Box != v.shownBox(r.Bank) {
		return false
	}
	if r.Bank {
		return v.BankSlots[r.Slot] != nil
	}
	return v.Cart[r.Slot] != nil
}

// bankEditor: the bar with the modes and buttons, then the two sets of boxes and the summary.
func (w *Window) bankEditor(gtx C) D {
	b := w.st.Bank
	if b == nil {
		return D{}
	}
	v := b.view()
	if !v.Ready {
		return D{}
	}
	w.bankPointer(gtx, b, &v)
	d := &w.bankDrag
	d.rects = d.rects[:0]

	m := op.Record(gtx.Ops)
	W := gtx.Constraints.Max.X
	bar := w.bankBar(gtx, &v)
	y := bar.Size.Y + gtx.Dp(14)
	sp := gtx.Dp(14)
	wide := W >= gtx.Dp(1060)
	sumW, gaps := 0, 1
	if wide {
		sumW, gaps = gtx.Dp(340), 2
	}
	panelW := (W - sumW - sp*gaps) / 2
	h := 0
	for _, bank := range []bool{true, false} { // the bank first: before a cartridge is loaded, its side is as tall
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
			pd = w.bankNoCart(g, &v)
		} else {
			pd = w.bankPanel(g, &v, bank, image.Pt(x, y))
		}
		st.Pop()
		h = max(h, pd.Size.Y)
	}
	if wide {
		st := op.Offset(image.Pt(2*(panelW+sp), y)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min.X, g.Constraints.Max.X, g.Constraints.Min.Y = sumW, sumW, 0
		sd := w.bankSummary(g, &v)
		st.Pop()
		h = max(h, sd.Size.Y)
	}
	y += h
	if !wide && v.SelMon != nil {
		y += sp
		st := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min.X, g.Constraints.Min.Y = W, 0
		sd := w.bankSummary(g, &v)
		st.Pop()
		y += sd.Size.Y
	}
	call := m.Stop()

	size := image.Pt(W, y)
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, d)
	switch {
	case d.dragging:
		pointer.CursorGrabbing.Add(gtx.Ops)
	case d.over && w.overMon(&v) && !v.Saving:
		pointer.CursorPointer.Add(gtx.Ops)
	}
	call.Add(gtx.Ops)
	if d.dragging && d.press != nil { // the Pokémon in the hand follows the pointer
		w.drawDragged(gtx, &v)
	}
	if d.boxing { // the box being drawn
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

func (w *Window) overMon(v *bankView) bool {
	r, ok := w.bankDrag.slotAt(w.bankDrag.at)
	return ok && w.bankHas(v, r)
}

func (w *Window) drawDragged(gtx C, v *bankView) {
	d := &w.bankDrag
	p := v.Cart[d.press.Slot]
	if d.press.Bank {
		p = v.BankSlots[d.press.Slot]
	}
	if p == nil || d.press.Box != v.shownBox(d.press.Bank) {
		return
	}
	s := gtx.Dp(56)
	st := op.Offset(image.Pt(int(d.at.X)-s/2, int(d.at.Y)-s/2)).Push(gtx.Ops)
	if imgOp, ok := w.bankIcons.get(v.Icons, p.IconIndex()); ok {
		drawIcon(gtx, imgOp, s)
	} else {
		fill(gtx, image.Pt(s, s), gtx.Dp(8), w.pal.accentSoft)
	}
	if n := v.Picked; v.Mode == modeMulti && v.isPicked(*d.press) && n > 1 { // how many go with it
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

func (v *bankView) isPicked(r slotRef) bool {
	if r.Box != v.shownBox(r.Bank) {
		return false
	}
	if r.Bank {
		return v.BankPick[r.Slot]
	}
	return v.CartPick[r.Slot]
}

// bankBar: the modes, what to do in the one chosen, what's changed, and Save.
func (w *Window) bankBar(gtx C, v *bankView) D {
	return w.card(gtx, func(gtx C) D {
		hint := map[string]string{
			modeMove: "Drag a Pokémon to another slot. One already there swaps places. Click one to see its summary.",
			modeMulti: "Drag a box over the slots to choose every Pokémon in it (hold Ctrl to add more), or click them one by one. " +
				"Then drag one of the chosen ones to where they should go.",
			modeRelease: "Click the Pokémon to release, or drag a box over them, then press Release. Once you save they're gone for good.",
		}[v.Mode]
		if !v.CartLoaded && v.Mode != modeRelease {
			hint += " Load a cartridge to move Pokémon to and from it."
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
				parts = append(parts, fmt.Sprintf("%d to the cartridge", c.ToCart))
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
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b("bank-mode:"+m), style, label, false, nil) })
			})
		}
		btn := func(id, style, label string) layout.FlexChild {
			return layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b(id), style, label, false, nil) })
			})
		}
		// what the chosen or marked Pokémon can do
		var actions []layout.FlexChild
		if n := v.Picked; n > 0 && !v.Saving {
			switch v.Mode {
			case modeMulti:
				actions = append(actions,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 14, font.SemiBold, w.pal.accent, fmt.Sprintf("%d chosen", n)) }),
					btn("bank-clear", "ghost", "Clear"))
			case modeRelease:
				actions = append(actions,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 14, font.SemiBold, w.pal.bad, fmt.Sprintf("%d marked", n)) }),
					btn("bank-release", "primary", fmt.Sprintf("Release %d", n)), btn("bank-clear", "ghost", "Clear"))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				kids := []layout.FlexChild{modeBtn(modeMove, "Move"), modeBtn(modeMulti, "Move Multi"), modeBtn(modeRelease, "Release"),
					layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} })}
				if c.Any && !v.Saving {
					kids = append(kids, btn("bank-undo", "ghost", "Undo changes"), layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b("bank-save"), "primary", "Save", false, icCheck) })
					}))
				}
				if !v.Saving {
					kids = append(kids, btn("bank-done", "secondary", "Done"))
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

// bankPanel: one side's box (the cartridge's or the bank's), with the arrows to turn it. base is
// where the panel is in the boxes' coordinates (for the slots' places).
func (w *Window) bankPanel(gtx C, v *bankView, bank bool, base image.Point) D {
	k := map[bool]string{false: "c", true: "b"}[bank]
	title := v.GameName
	sub := fmt.Sprintf("%s · ID %05d", v.Trainer, v.TID)
	name, count := v.CartName, fmt.Sprintf("Box %d of %d", v.CartBox+1, g3BoxCount)
	slots, picks := v.Cart, v.CartPick
	if bank {
		title, sub = "Pokémon Bank", fmt.Sprintf("On this PC · %d Pokémon", v.InBank)
		name, count = v.BankName, fmt.Sprintf("Box %d of %d", v.BankBox+1, v.BankBoxes)
		slots, picks = v.BankSlots, v.BankPick
	}
	pad := gtx.Dp(22)
	return w.card(gtx, func(gtx C) D {
		head := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { // the same height on both sides, so the boxes line up
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
						return w.button(gtx, w.b("bank-all:"+k), "ghost", "Select all", false, nil)
					}),
					layout.Rigid(func(gtx C) D {
						if !bank {
							return D{}
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								if v.BankBox != v.BankBoxes-1 || v.BankBoxes >= bankMaxBoxes {
									return D{}
								}
								return w.button(gtx, w.b("bank-add"), "ghost", "+ Box", false, nil)
							}),
							layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("bank-folder"), "ghost", "Folder", false, icFolder) }))
					}))
			}),
			layout.Rigid(gap(10)),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("bank-prev:"+k), "secondary", "‹", false, nil) }),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.centered(gtx, 15, font.SemiBold, w.pal.text, name) }),
							layout.Rigid(func(gtx C) D { return w.centered(gtx, 12, font.Normal, w.pal.muted, count) }))
					}),
					layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("bank-next:"+k), "secondary", "›", false, nil) }))
			}),
			layout.Rigid(gap(10)))
		gy := head.Size.Y
		st := op.Offset(image.Pt(0, gy)).Push(gtx.Ops)
		grid := w.bankGrid(gtx, v, bank, slots, picks, base.Add(image.Pt(pad, pad+gy)))
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
			foot = fmt.Sprintf("%d of %d slots used in this box.", n, g3BoxSlots)
		}
		st = op.Offset(image.Pt(0, y+gtx.Dp(10))).Push(gtx.Ops)
		w.labelMax(gtx, 12, font.Normal, w.pal.muted, foot, 1)
		st.Pop()
		y += gtx.Dp(10) + gtx.Dp(18) // the same on both sides
		return D{Size: image.Pt(gtx.Constraints.Max.X, y)}
	})
}

// bankNoCart: the cartridge's side before a cartridge is loaded.
func (w *Window) bankNoCart(gtx C, v *bankView) D {
	minY := gtx.Constraints.Min.Y
	return w.box(gtx, w.pal.surface, w.pal.border, 14, 22, func(gtx C) D {
		gtx.Constraints.Min.Y = max(0, minY-2*gtx.Dp(22))
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return icon(gtx, icGamepad, w.pal.muted, 44) }),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D { return w.centered(gtx, 17, font.SemiBold, w.pal.text, "Your cartridge") }),
				layout.Rigid(gap(6)),
				layout.Rigid(func(gtx C) D {
					return w.centered(gtx, 13.5, font.Normal, w.pal.muted, "Look through the bank as it is, or load the boxes of your Pokémon Emerald, FireRed or LeafGreen "+
						"cartridge over GB-Link, or a save file (a flash cart's SD card or an emulator), to move Pokémon to and from it.")
				}),
				layout.Rigid(gap(16)),
				layout.Rigid(func(gtx C) D {
					if v.Loading {
						return w.spinner(gtx, 24)
					}
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return w.button(gtx, w.b("bank-load"), "primary", "Load from cartridge", false, icDownload)
						}),
						layout.Rigid(gap(10)),
						layout.Rigid(func(gtx C) D {
							return w.button(gtx, w.b("bank-file"), "secondary", "Open a save file…", false, icFolder)
						}))
				}))
		})
	})
}

// bankGrid: the 30 slots, 6 across and 5 down. origin is where it is in the boxes' coordinates.
func (w *Window) bankGrid(gtx C, v *bankView, bank bool, slots [g3BoxSlots]*Pk3, picks [g3BoxSlots]bool, origin image.Point) D {
	sp := gtx.Dp(6)
	size := min((gtx.Constraints.Max.X-5*sp)/6, gtx.Dp(64))
	left := (gtx.Constraints.Max.X - 6*size - 5*sp) / 2
	box := v.shownBox(bank)
	d := &w.bankDrag
	var target *slotRef
	if d.dragging {
		if r, ok := d.slotAt(d.at); ok {
			target = &r
		}
	}
	inBox := map[slotRef]bool{}
	if d.boxing {
		for _, r := range d.inBox() { // (last frame's places: the same as this frame's)
			inBox[r] = true
		}
	}
	for i := 0; i < g3BoxSlots; i++ {
		x, y := left+i%6*(size+sp), i/6*(size+sp)
		here := slotRef{Bank: bank, Box: box, Slot: i}
		d.rects = append(d.rects, bankRect{here, image.Rectangle{Min: origin.Add(image.Pt(x, y)), Max: origin.Add(image.Pt(x+size, y+size))}})
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(size, size))
		w.bankSlot(g, v, slots[i], here, picks[i] || inBox[here] && slots[i] != nil, target != nil && *target == here)
		st.Pop()
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, 5*size+4*sp)}
}

func (w *Window) bankSlot(gtx C, v *bankView, p *Pk3, here slotRef, picked, target bool) {
	d := &w.bankDrag
	size := gtx.Constraints.Min
	held := v.Mode == modeMove && v.Held != nil && *v.Held == here
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
	case picked || held:
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
	if imgOp, ok := w.bankIcons.get(v.Icons, p.IconIndex()); ok {
		px := size.X * 9 / 10
		o := op.Offset(image.Pt((size.X-px)/2, (size.Y-px)/2-size.Y/16)).Push(gtx.Ops)
		drawIcon(gtx, imgOp, px)
		o.Pop()
	} else {
		name := v.Data.speciesName(p.Species)
		if p.Egg {
			name = "Egg"
		}
		layout.Center.Layout(gtx, func(gtx C) D {
			return layout.UniformInset(4).Layout(gtx, func(gtx C) D { return w.labelMax(gtx, 11, font.SemiBold, w.pal.text, name, 2) })
		})
	}
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
	case p.Shiny() && !p.Egg:
		mark("★", w.pal.warn, true)
	}
	if picked {
		if v.Mode == modeRelease {
			mark("✕", w.pal.bad, false)
		} else {
			mark("✓", w.pal.accent, false)
		}
	}
	if p.Item != 0 && !p.Egg { // holding an item: a little dot in the corner
		dot := gtx.Dp(7)
		o := op.Offset(image.Pt(size.X-dot-gtx.Dp(4), size.Y-dot-gtx.Dp(4))).Push(gtx.Ops)
		fill(gtx, image.Pt(dot, dot), dot/2, w.pal.warn)
		o.Pop()
	}
	if held || moving { // in the hand: shown faded where it was
		fill(gtx, size, r, color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 150})
	}
}

// drawIcon draws a 32x32 icon px pixels wide, its dots kept square.
func drawIcon(gtx C, imgOp paint.ImageOp, px int) {
	s := float32(px) / 32
	st := op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(s, s))).Push(gtx.Ops)
	cl := clip.Rect{Max: image.Pt(32, 32)}.Push(gtx.Ops)
	imgOp.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	st.Pop()
}

// bankSummary: the chosen Pokémon, as PKHeX's summary shows it.
func (w *Window) bankSummary(gtx C, v *bankView) D {
	return w.card(gtx, func(gtx C) D {
		p := v.SelMon
		if p == nil {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, "Summary") }),
				layout.Rigid(gap(6)),
				layout.Rigid(func(gtx C) D {
					return w.label(gtx, 13.5, font.Normal, w.pal.muted, "Click a Pokémon to see everything about it here: its nature, IVs, EVs and stats, moves, held item, where it was met and more.")
				}))
		}
		d := v.Data
		var rows []layout.FlexChild
		add := func(wd layout.Widget) { rows = append(rows, layout.Rigid(wd)) }
		text := func(size unit.Sp, weight font.Weight, c color.NRGBA, t string) {
			add(func(gtx C) D { return w.label(gtx, size, weight, c, t) })
		}
		kv := func(k, val string) {
			if val == "" {
				return
			}
			add(func(gtx C) D {
				return layout.Inset{Top: 3}.Layout(gtx, func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Dp(100)
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
		species := d.speciesName(p.Species)
		nick := decodeGameText(p.Nick, p.Japanese())
		valid := p.Valid()
		level := d.level(p)

		// the head: icon, name, species, level
		add(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(72)
					fill(gtx, image.Pt(s, s), gtx.Dp(12), w.pal.surface2)
					if imgOp, ok := w.bankIcons.get(v.Icons, p.IconIndex()); ok {
						drawIcon(gtx, imgOp, s)
					}
					return D{Size: image.Pt(s, s)}
				}),
				layout.Rigid(gap(14)),
				layout.Flexed(1, func(gtx C) D {
					head, sub := nick, species
					switch {
					case !valid:
						head, sub = "Bad Egg", "Its data is damaged"
					case p.Egg:
						head, sub = "Egg", species
					case nick == "" || strings.EqualFold(nick, species):
						head, sub = species, ""
					}
					if g := d.gender(p); g != "" && valid && !p.Egg {
						head += map[string]string{"M": " ♂", "F": " ♀"}[g]
					}
					line2 := []string{}
					if sub != "" {
						line2 = append(line2, sub)
					}
					if valid && !p.Egg {
						line2 = append(line2, fmt.Sprintf("Lv %d", level))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 17, font.SemiBold, w.pal.text, head, 1) }),
						layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 13.5, font.Normal, w.pal.muted, strings.Join(line2, " · "), 1) }),
						layout.Rigid(func(gtx C) D {
							if !p.Shiny() || p.Egg {
								return D{}
							}
							return w.label(gtx, 13, font.SemiBold, w.pal.warn, "★ Shiny")
						}))
				}))
		})
		if !valid {
			text(13, font.Normal, w.pal.bad, "The checksum is wrong, so the game would show a Bad Egg. It's kept exactly as it is.")
		} else {
			section("About")
			nat := natureNamesEN[p.Nature()]
			if up, down := natureEffect(p.Nature()); up != "" {
				nat += " (+" + up + " −" + down + ")"
			}
			kv("Nature", nat)
			kv("Ability", d.ability(p))
			if !p.Egg {
				item := d.itemName(p.Item)
				if item == "" {
					item = "None"
				}
				kv("Held item", item)
			}
			ot := decodeGameText(p.OTName, p.Japanese())
			if p.OTFemale {
				ot += " ♀"
			} else {
				ot += " ♂"
			}
			kv("OT", ot)
			kv("ID / SID", fmt.Sprintf("%05d / %05d", p.TID(), p.SID()))
			if p.Egg {
				kv("Egg cycles", itoa(p.Friend))
			} else {
				kv("Friendship", itoa(p.Friend))
			}
			kv("Language", languageNames[p.Lang])

			section("Met")
			if p.MetLevel == 0 && p.MetPlace < 0xFD {
				kv("Hatched at", d.placeName(p.MetPlace))
			} else {
				kv("Met at", d.placeName(p.MetPlace))
				if p.MetLevel > 0 {
					kv("Met level", itoa(p.MetLevel))
				}
			}
			kv("Game", metGameNames[p.MetGame])
			kv("Ball", ballNames[p.Ball])

			if !p.Egg {
				section("Stats")
				base, haveBase := d.base(p.Species)
				stats := p.Stats(base, level)
				up, down := natureEffect(p.Nature())
				add(func(gtx C) D {
					names := []string{"HP", "Atk", "Def", "SpA", "SpD", "Spe"}
					order := []int{0, 1, 2, 4, 5, 3} // stored HP Atk Def Spe SpA SpD
					cell := func(t string, c color.NRGBA, wgt font.Weight) layout.FlexChild {
						return layout.Flexed(1, func(gtx C) D { return w.label(gtx, 13, wgt, c, t) })
					}
					row := func(kids ...layout.FlexChild) layout.FlexChild {
						return layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return layout.Flex{}.Layout(gtx, kids...) })
						})
					}
					lines := []layout.FlexChild{row(cell("", w.pal.muted, font.Normal), cell("Base", w.pal.muted, font.SemiBold),
						cell("IV", w.pal.muted, font.SemiBold), cell("EV", w.pal.muted, font.SemiBold), cell("Stat", w.pal.muted, font.SemiBold))}
					for i, n := range names {
						s := order[i]
						c := w.pal.text
						if n == up {
							c = w.pal.bad
						} else if n == down {
							c = w.pal.accent
						}
						b, st := "?", "?"
						if haveBase {
							b, st = itoa(base[s]), itoa(stats[s])
						}
						ivc := w.pal.text
						if p.IVs[s] == 31 {
							ivc = w.pal.ok
						}
						lines = append(lines, row(cell(n, c, font.SemiBold), cell(b, w.pal.text, font.Normal),
							cell(itoa(p.IVs[s]), ivc, font.Normal), cell(itoa(p.EVs[s]), w.pal.text, font.Normal), cell(st, w.pal.text, font.SemiBold)))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, lines...)
				})
				hp, pw := p.HiddenPower()
				ivSum, evSum := 0, 0
				for i := range p.IVs {
					ivSum += p.IVs[i]
					evSum += p.EVs[i]
				}
				kv("Totals", fmt.Sprintf("IVs %d/186 · EVs %d/510", ivSum, evSum))
				kv("Hidden Power", fmt.Sprintf("%s %d", hp, pw))

				section("Moves")
				some := false
				for i, m := range p.Moves {
					if m == 0 {
						continue
					}
					some = true
					pp := fmt.Sprintf("PP %d", p.PP[i])
					if ups := p.PPUps >> (2 * i) & 3; ups > 0 {
						pp += fmt.Sprintf(" (PP Up ×%d)", ups)
					}
					kv(d.moveName(m), pp)
				}
				if !some {
					text(13, font.Normal, w.pal.muted, "No moves")
				}
			}

			section("More")
			switch {
			case p.Pokerus == 0:
				kv("Pokérus", "Never had it")
			case p.Pokerus&15 == 0:
				kv("Pokérus", "Cured")
			default:
				kv("Pokérus", "Infected")
			}
			kv("Experience", itoa(int(p.Exp)))

			section(fmt.Sprintf("Ribbons (%d)", p.RibbonCount()))
			if r := p.RibbonNames(); len(r) > 0 {
				text(13, font.Normal, w.pal.text, strings.Join(r, ", "))
			} else {
				text(13, font.Normal, w.pal.muted, "No ribbons")
			}
		}
		kv("PID", fmt.Sprintf("%08X", p.PID))
		if m := v.SelMeta; m != nil && m.Added != "" {
			kv("In the bank", "since "+m.Added)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	})
}

// statusCard: what's happening, with a spinner (the bank's boxes stay below it).
func (w *Window) statusCard(gtx C) D {
	return w.card(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return w.spinner(gtx, 20) }),
			layout.Rigid(gap(12)),
			layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.text, w.st.Status) }))
	})
}
