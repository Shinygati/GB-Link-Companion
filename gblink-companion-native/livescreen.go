// The live view of the GBA's status screen, shown in the app's window while you play (the
// "Show the GBA's screen here" setting). It draws the copy kept in gbascreen.go, so nothing
// extra is sent to the GBA and the link isn't slowed down.
package main

import (
	"image"
	"image/color"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// liveView: the window's picture of the screen, rebuilt only when the screen has changed.
type liveView struct {
	scr *gbaScreen // the screen the picture was made from
	rev uint64
	op  paint.ImageOp
	ok  bool // op holds a picture (else no status screen is up)
}

// liveScreen: the screen copy of the game being played, or nil when none is running.
func (a *App) liveScreen() *gbaScreen {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil || s.helper == nil {
		return nil
	}
	return &s.helper.shot
}

// update brings the picture up to date. It reports whether a status screen is up.
func (v *liveView) update(scr *gbaScreen) bool {
	if scr == nil {
		v.scr, v.ok = nil, false
		return false
	}
	if rev := scr.revision(); scr != v.scr || rev != v.rev {
		v.scr, v.rev = scr, rev
		if img := scr.image(1); img != nil {
			v.op, v.ok = paint.NewImageOp(img), true
		} else {
			v.ok = false
		}
	}
	return v.ok
}

// liveCard: the GBA's status screen, as big as fits (up to 3x), with its dots kept square.
func (w *Window) liveCard(gtx C) D {
	up := w.live.update(w.app.liveScreen())
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)}) // look for changes 10 times a second
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		return w.card(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, "GBA screen") }),
						layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("live-pop"), "ghost", "Pop out", false, nil) }),
						layout.Rigid(gap(4)),
						layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("live-hide"), "ghost", "Hide", false, nil) }))
				}),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D {
					if !up {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return w.label(gtx, 13.5, font.Normal, w.pal.muted,
							"The GBA isn't showing a status screen. Hold SELECT on it and pick one.")
					}
					ps := w.live.op.Size()
					zoom := max(1, min(3, gtx.Constraints.Max.X/gtx.Dp(scW)))
					sc := float32(zoom) * float32(gtx.Dp(1))
					size := image.Pt(int(float32(ps.X)*sc), int(float32(ps.Y)*sc))
					st := op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sc, sc))).Push(gtx.Ops)
					cl := clip.Rect{Max: ps}.Push(gtx.Ops)
					w.live.op.Filter = paint.FilterNearest
					w.live.op.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					cl.Pop()
					st.Pop()
					return D{Size: size}
				}))
		})
	})
}

// ------------------------------------------------------------------------------ own window
// livePop: the GBA's screen in a window of its own (the card's "Pop out"). It can be moved, sized
// and kept beside mGBA; it closes with the app.
type livePop struct {
	a    *App
	w    *app.Window
	th   *material.Theme
	view liveView
}

// openLivePop opens the window, or brings it to the front if it's open already.
func (a *App) openLivePop() {
	a.winMu.Lock()
	if p := a.pop; p != nil {
		a.winMu.Unlock()
		p.w.Perform(system.ActionRaise)
		return
	}
	if fontCache.faces == nil {
		fontCache.faces, fontCache.face = loadFonts()
	}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(fontCache.faces))
	th.Face = fontCache.face
	p := &livePop{a: a, w: new(app.Window), th: th}
	a.pop = p
	a.winMu.Unlock()
	p.w.Option(app.Title("GB-Link Companion: GBA screen"), app.Size(unit.Dp(720), unit.Dp(480)), app.MinSize(unit.Dp(240), unit.Dp(160)))
	go p.loop()
}

// closeLivePop closes the window if it's open.
func (a *App) closeLivePop() {
	a.winMu.Lock()
	p := a.pop
	a.winMu.Unlock()
	if p != nil {
		p.w.Perform(system.ActionClose)
	}
}

func (p *livePop) loop() {
	var ops op.Ops
	for {
		switch e := p.w.Event().(type) {
		case app.DestroyEvent:
			p.a.winMu.Lock()
			if p.a.pop == p {
				p.a.pop = nil
			}
			p.a.winMu.Unlock()
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			p.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// layout draws the screen as big as fits, in whole-number steps when it can so the dots stay sharp.
func (p *livePop) layout(gtx C) {
	paint.Fill(gtx.Ops, color.NRGBA{R: 6, G: 8, B: 16, A: 255})
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if !p.view.update(p.a.liveScreen()) {
		layout.Center.Layout(gtx, func(gtx C) D {
			l := material.Label(p.th, 15, "The GBA isn't showing a status screen.\nHold SELECT on it and pick one.")
			l.Color = color.NRGBA{R: 170, G: 175, B: 190, A: 255}
			l.Alignment = text.Middle
			l.Font.Typeface = fontCache.face
			return l.Layout(gtx)
		})
		return
	}
	ps := p.view.op.Size()
	sc := min(float32(W)/float32(ps.X), float32(H)/float32(ps.Y))
	if sc >= 1 {
		sc = float32(int(sc))
	}
	w, h := int(float32(ps.X)*sc), int(float32(ps.Y)*sc)
	off := op.Offset(image.Pt((W-w)/2, (H-h)/2)).Push(gtx.Ops)
	st := op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sc, sc))).Push(gtx.Ops)
	cl := clip.Rect{Max: ps}.Push(gtx.Ops)
	p.view.op.Filter = paint.FilterNearest
	p.view.op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	st.Pop()
	off.Pop()
}
