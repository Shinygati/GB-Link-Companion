// The window, drawn with Gio (no web browser): game list, the play session, settings.
package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/io/clipboard"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

//go:embed assets/icon.png
var iconPNG []byte

//go:embed assets/gba.png
var gbaPNG []byte

// The cartridge shell drawn for each game (tools/mkcart.py), at two sizes so it scales down cleanly;
// the game's label is painted into its label window.
//
//go:embed assets/cart_s.png
var cartSmallPNG []byte

//go:embed assets/cart_l.png
var cartLargePNG []byte

// ------------------------------------------------------------------------------- colours
type palette struct {
	bg, surface, surface2, text, muted, border      color.NRGBA
	accent, accentHover, accentSoft                 color.NRGBA
	ok, okSoft, warn, warnSoft, bad, badSoft, scrim color.NRGBA
	dark                                            bool
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

var lightPalette = palette{
	bg: rgb(0xf4f5f8), surface: rgb(0xffffff), surface2: rgb(0xeef0f5), text: rgb(0x1b1e27), muted: rgb(0x646a7a),
	border: rgb(0xdfe2ea), accent: rgb(0x4f46e5), accentHover: rgb(0x4338ca), accentSoft: rgb(0xe9e8fd),
	ok: rgb(0x138a5b), okSoft: rgb(0xe2f5ec), warn: rgb(0xb26a00), warnSoft: rgb(0xfff1da), bad: rgb(0xc2362f),
	badSoft: rgb(0xfde6e4), scrim: color.NRGBA{10, 12, 20, 115},
}

var darkPalette = palette{
	bg: rgb(0x14161c), surface: rgb(0x1d2029), surface2: rgb(0x262a35), text: rgb(0xe9ebf1), muted: rgb(0x9aa0b1),
	border: rgb(0x2f3442), accent: rgb(0x7c74ff), accentHover: rgb(0x918aff), accentSoft: rgb(0x2b2a4a),
	ok: rgb(0x3fcf8e), okSoft: rgb(0x173326), warn: rgb(0xf0b04a), warnSoft: rgb(0x3a2c13), bad: rgb(0xff7a70),
	badSoft: rgb(0x3d1c1a), scrim: color.NRGBA{0, 0, 0, 140}, dark: true,
}

// ---------------------------------------------------------------------------------- fonts
// Segoe UI on Windows, so the app looks like the rest of Windows; the Go fonts elsewhere.
func loadFonts() ([]font.FontFace, font.Typeface) {
	coll := gofont.Collection()
	var dirs, sets [][2]string
	if runtime.GOOS == "windows" {
		dirs = append(dirs, [2]string{filepath.Join(os.Getenv("WINDIR"), "Fonts"), "Segoe UI"})
		sets = [][2]string{{"segoeui.ttf", "400"}, {"seguisb.ttf", "600"}, {"segoeuib.ttf", "700"}}
	} else {
		dirs = append(dirs, [2]string{"/usr/share/fonts/truetype/noto", "Noto Sans"})
		sets = [][2]string{{"NotoSans-Regular.ttf", "400"}, {"NotoSans-SemiBold.ttf", "600"}, {"NotoSans-Bold.ttf", "700"}}
	}
	for _, d := range dirs {
		var faces []font.FontFace
		for _, s := range sets {
			b, err := os.ReadFile(filepath.Join(d[0], s[0]))
			if err != nil {
				continue
			}
			f, err := opentype.Parse(b)
			if err != nil {
				continue
			}
			w := font.Normal
			switch s[1] {
			case "600":
				w = font.SemiBold
			case "700":
				w = font.Bold
			}
			faces = append(faces, font.FontFace{Font: font.Font{Typeface: font.Typeface(d[1]), Weight: w}, Face: f})
		}
		if len(faces) > 0 {
			return append(faces, coll...), font.Typeface(d[1])
		}
	}
	return coll, "Go"
}

func systemDark() bool {
	if v := os.Getenv("GBLINK_THEME"); v != "" {
		return v == "dark"
	}
	return osDarkMode()
}

// ------------------------------------------------------------------------------- icons
func mustIcon(data []byte) *widget.Icon {
	ic, err := widget.NewIcon(data)
	if err != nil {
		panic(err)
	}
	return ic
}

var (
	icSettings = mustIcon(icons.ActionSettings)
	icSearch   = mustIcon(icons.ActionSearch)
	icPlay     = mustIcon(icons.AVPlayArrow)
	icCheck    = mustIcon(icons.NavigationCheck)
	icPower    = mustIcon(icons.ActionPowerSettingsNew)
	icGamepad  = mustIcon(icons.HardwareVideogameAsset)
	icSync     = mustIcon(icons.NotificationSync)
	icWarn     = mustIcon(icons.AlertErrorOutline)
	icDots     = mustIcon(icons.NavigationMoreHoriz)
	icRemove   = mustIcon(icons.ContentRemove)
	icExpand   = mustIcon(icons.NavigationExpandMore)
	icCollapse = mustIcon(icons.NavigationExpandLess)
	icFolder   = mustIcon(icons.FileFolderOpen)
	icDownload = mustIcon(icons.FileFileDownload)
	icUpload   = mustIcon(icons.FileFileUpload)
	icCopy     = mustIcon(icons.ContentContentCopy)
)

// ------------------------------------------------------------------------------ the window
type Window struct {
	app      *App
	w        *app.Window
	th       *material.Theme
	face     font.Typeface
	pal      palette
	logo     paint.ImageOp
	gbaPhoto paint.ImageOp
	cartS    paint.ImageOp
	cartL    paint.ImageOp

	st       State
	selected string
	inSetup  int // 0 unknown, 1 showing setup, 2 done

	page, list, logList widget.List
	search              widget.Editor
	games               map[string]*widget.Clickable
	prompt              map[string]*widget.Clickable
	btn                 map[string]*widget.Clickable
	setCtl, setAsk      widget.Bool
	setSeed             widget.Bool
	setClock            widget.Bool
	liveOn              bool     // the GBA's screen is shown in the window (the "Show GBA screen" button)
	live                liveView // its picture (livescreen.go)
	settingsOpen        bool
	wcOpen              bool   // the Wonder Cards list (wcgui.go)
	wcSel               string // the card whose description is open
	wcList              widget.List
	setList             widget.List // Settings, when it doesn't fit
	detailsOpen         bool
	pending             chan func() // work to run on the window's own goroutine (tests)
}

func (a *App) redraw() {
	a.winMu.Lock()
	w := a.win
	a.winMu.Unlock()
	if w != nil {
		w.w.Invalidate()
	}
}

// raise brings the window to the front, or opens a new one if it was closed.
func (a *App) raise() {
	a.winMu.Lock()
	w := a.win
	a.winMu.Unlock()
	if w != nil {
		w.w.Perform(system.ActionRaise)
		w.w.Invalidate()
		return
	}
	a.openWindow()
}

var fontCache struct {
	faces []font.FontFace
	face  font.Typeface
}

func (a *App) openWindow() {
	a.winMu.Lock()
	if a.win != nil {
		a.winMu.Unlock()
		return
	}
	if fontCache.faces == nil {
		fontCache.faces, fontCache.face = loadFonts()
	}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(fontCache.faces))
	th.Face = fontCache.face
	win := &Window{app: a, w: new(app.Window), th: th, face: fontCache.face,
		games: map[string]*widget.Clickable{}, prompt: map[string]*widget.Clickable{}, btn: map[string]*widget.Clickable{},
		pending: make(chan func(), 64)}
	win.page.Axis = layout.Vertical
	win.list.Axis = layout.Vertical
	win.logList.Axis = layout.Vertical
	win.wcList.Axis = layout.Vertical
	win.setList.Axis = layout.Vertical
	win.search.SingleLine = true
	if img, err := png.Decode(bytes.NewReader(iconPNG)); err == nil {
		win.logo = paint.NewImageOp(img)
	}
	if img, err := png.Decode(bytes.NewReader(gbaPNG)); err == nil {
		win.gbaPhoto = paint.NewImageOp(img)
	}
	if img, err := png.Decode(bytes.NewReader(cartSmallPNG)); err == nil {
		win.cartS = paint.NewImageOp(img)
	}
	if img, err := png.Decode(bytes.NewReader(cartLargePNG)); err == nil {
		win.cartL = paint.NewImageOp(img)
	}
	a.win = win
	a.winMu.Unlock()
	win.w.Option(app.Title("GB-Link Companion"), app.Size(unit.Dp(920), unit.Dp(740)), app.MinSize(unit.Dp(600), unit.Dp(520)))
	go win.loop()
}

func (w *Window) selectROM(rom string) { w.do(func() { w.selected = rom }) }

// do runs f on the window's goroutine before the next frame.
func (w *Window) do(f func()) {
	w.pending <- f
	w.w.Invalidate()
}

func (w *Window) loop() {
	var ops op.Ops
	for {
		switch e := w.w.Event().(type) {
		case app.DestroyEvent:
			a := w.app
			a.winMu.Lock()
			a.win = nil
			a.winMu.Unlock()
			a.closeLivePop()
			if !a.busy() {
				os.Exit(0)
			}
			// a game is still running or the save is being written: keep going in the
			// background; the window comes back if it's needed (ui.Raise). Before that, stop.
			a.closeWhileBusy()
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			w.pal = lightPalette
			if systemDark() {
				w.pal = darkPalette
			}
			w.th.Palette = material.Palette{Bg: w.pal.surface, Fg: w.pal.text, ContrastBg: w.pal.accent, ContrastFg: color.NRGBA{255, 255, 255, 255}}
			w.st = w.app.ui.Snapshot()
			w.frame(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// --------------------------------------------------------------------------- drawing kit
func (w *Window) b(id string) *widget.Clickable {
	c := w.btn[id]
	if c == nil {
		c = new(widget.Clickable)
		w.btn[id] = c
	}
	return c
}

func fill(gtx C, size image.Point, r int, c color.NRGBA) {
	r = min(r, size.X/2, size.Y/2) // a bigger radius draws garbage
	defer clip.UniformRRect(image.Rectangle{Max: size}, r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// box draws a rounded rectangle (with an optional border) behind its content.
func (w *Window) box(gtx C, bg, border color.NRGBA, radius, pad unit.Dp, content layout.Widget) D {
	r := gtx.Dp(radius)
	m := op.Record(gtx.Ops)
	dims := layout.UniformInset(pad).Layout(gtx, content)
	call := m.Stop()
	if border.A > 0 {
		fill(gtx, dims.Size, r, border)
		bw := gtx.Dp(1)
		st := op.Offset(image.Pt(bw, bw)).Push(gtx.Ops)
		fill(gtx, dims.Size.Sub(image.Pt(2*bw, 2*bw)), max(r-bw, 0), bg)
		st.Pop()
	} else {
		fill(gtx, dims.Size, r, bg)
	}
	call.Add(gtx.Ops)
	return dims
}

func (w *Window) card(gtx C, content layout.Widget) D {
	return w.box(gtx, w.pal.surface, w.pal.border, 14, 22, content)
}

func (w *Window) label(gtx C, size unit.Sp, weight font.Weight, c color.NRGBA, txt string) D {
	l := material.Label(w.th, size, txt)
	l.Color = c
	l.Font.Weight = weight
	l.Font.Typeface = w.face
	return l.Layout(gtx)
}

func (w *Window) labelMax(gtx C, size unit.Sp, weight font.Weight, c color.NRGBA, txt string, lines int) D {
	l := material.Label(w.th, size, txt)
	l.Color = c
	l.Font.Weight = weight
	l.Font.Typeface = w.face
	l.MaxLines = lines
	return l.Layout(gtx)
}

func (w *Window) centered(gtx C, size unit.Sp, weight font.Weight, c color.NRGBA, txt string) D {
	l := material.Label(w.th, size, txt)
	l.Color = c
	l.Font.Weight = weight
	l.Font.Typeface = w.face
	l.Alignment = text.Middle
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return l.Layout(gtx)
}

func icon(gtx C, ic *widget.Icon, c color.NRGBA, size unit.Dp) D {
	s := gtx.Dp(size)
	gtx.Constraints = layout.Exact(image.Pt(s, s))
	return ic.Layout(gtx, c)
}

func gap(v unit.Dp) layout.Widget {
	return func(gtx C) D { return D{Size: image.Pt(gtx.Dp(v), gtx.Dp(v))} }
}

// button styles: primary, secondary, ghost
func (w *Window) button(gtx C, cl *widget.Clickable, style, txt string, big bool, ic *widget.Icon) D {
	gtx.Constraints.Min = image.Point{}
	return cl.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg, fg, border := w.pal.accent, color.NRGBA{255, 255, 255, 255}, color.NRGBA{}
		switch style {
		case "secondary":
			bg, fg, border = w.pal.surface, w.pal.text, w.pal.border
			if cl.Hovered() {
				bg = w.pal.surface2
			}
		case "ghost":
			bg, fg = color.NRGBA{}, w.pal.accent
			if cl.Hovered() {
				bg = w.pal.accentSoft
			}
		default:
			if cl.Hovered() {
				bg = w.pal.accentHover
			}
		}
		padV, padH, size, rad := unit.Dp(9), unit.Dp(16), unit.Sp(14.5), unit.Dp(10)
		if big {
			padV, padH, size, rad = 12, 22, 16, 12
		}
		return w.box(gtx, bg, border, rad, 0, func(gtx C) D {
			return layout.Inset{Top: padV, Bottom: padV, Left: padH, Right: padH}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						if ic == nil {
							return D{}
						}
						return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return icon(gtx, ic, fg, 20) })
					}),
					layout.Rigid(func(gtx C) D { return w.label(gtx, size, font.SemiBold, fg, txt) }))
			})
		})
	})
}

// width-limited, centred column
func column(gtx C, maxW unit.Dp, wd layout.Widget) D {
	avail := gtx.Constraints.Max.X
	cw := min(avail, gtx.Dp(maxW))
	gtx.Constraints.Max.X, gtx.Constraints.Min.X = cw, cw
	defer op.Offset(image.Pt((avail-cw)/2, 0)).Push(gtx.Ops).Pop()
	d := wd(gtx)
	d.Size.X = avail
	return d
}

// cartArt: a little coloured cartridge with the game's initials.
// cartLabels are the label colours of cartridges everyone knows by colour (by game code); other
// games get a colour of their own from their code.
var cartLabels = map[string]color.NRGBA{
	"AXV": {196, 36, 52, 255}, // Ruby
	"AXP": {36, 84, 196, 255}, // Sapphire
	"BPE": {24, 150, 96, 255}, // Emerald
	"BPR": {226, 88, 40, 255}, // FireRed
	"BPG": {88, 170, 60, 255}, // LeafGreen
}

// cartArt draws a game as a GBA-shaped cartridge with its label colour and initials; size is its height.
func (w *Window) cartArt(gtx C, g *GameView, size unit.Dp) D {
	name, code := "?", ""
	if g != nil {
		name, code = g.Name, g.Code
	}
	ht := gtx.Dp(size)
	wd := ht * 8 / 5 // the cartridge is 1.6 times as wide as it is tall
	shell := w.cartS
	if wd > shell.Size().X {
		shell = w.cartL
	}
	if shell.Size().X > 0 {
		sc := float32(wd) / float32(shell.Size().X)
		t := op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sc, sc))).Push(gtx.Ops)
		c := clip.Rect{Max: shell.Size()}.Push(gtx.Ops)
		shell.Filter = paint.FilterLinear
		shell.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		c.Pop()
		t.Pop()
	}
	// the label window (as drawn by tools/mkcart.py): x 0.095..0.855, y 0.255..0.845
	label := image.Rect(wd*95/1000, ht*255/1000, wd*855/1000, ht*845/1000)
	col, ok := color.NRGBA{}, false
	if len(code) >= 3 {
		col, ok = cartLabels[code[:3]]
	}
	if !ok {
		h := uint32(0)
		key := code
		if key == "" {
			key = name
		}
		for _, c := range key {
			h = h*31 + uint32(c)
		}
		col = hsl(float64(h%360), .55, .45)
	}
	st := op.Offset(label.Min).Push(gtx.Ops)
	r := max(1, ht*24/1000)
	fill(gtx, label.Size(), r, col)
	fill(gtx, image.Pt(label.Dx(), label.Dy()*36/100), r, color.NRGBA{255, 255, 255, 36}) // lighter top band
	gtx2 := gtx
	gtx2.Constraints = layout.Exact(label.Size())
	layout.Center.Layout(gtx2, func(gtx C) D {
		return w.label(gtx, unit.Sp(float32(size)*.36), font.Bold, color.NRGBA{255, 255, 255, 255}, initials(name))
	})
	st.Pop()
	return D{Size: image.Pt(wd, ht)}
}

func initials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var out []rune
	for _, wd := range words {
		switch strings.ToLower(wd) {
		case "pokemon", "pokémon", "the", "of", "and", "version":
			continue
		}
		out = append(out, unicode.ToUpper([]rune(wd)[0]))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 && len(words) > 0 {
		out = []rune{unicode.ToUpper([]rune(words[0])[0])}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func hsl(h, s, l float64) color.NRGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	return color.NRGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}

func shade(c color.NRGBA, f float64) color.NRGBA {
	return color.NRGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), c.A}
}

// ------------------------------------------------------------------------------- the frame
func (w *Window) frame(gtx C) {
	paint.Fill(gtx.Ops, w.pal.bg)
	for done := false; !done; {
		select {
		case f := <-w.pending:
			f()
		default:
			done = true
		}
	}
	st := w.st
	home := st.Phase == "home"
	setup := home && w.needsSetup()
	w.handleClicks(gtx, setup)

	if home && !setup {
		w.filterGames()
	}
	showBar := home && !setup && w.selectedGame() != nil
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(w.topbar),
		layout.Rigid(w.disclaimer),
		layout.Flexed(1, func(gtx C) D {
			switch {
			case setup:
				return w.pageOf(gtx, w.setupView())
			case home:
				return w.homeView(gtx)
			default:
				return w.pageOf(gtx, w.sessionView(gtx))
			}
		}),
		layout.Rigid(func(gtx C) D {
			if !showBar {
				return D{}
			}
			return w.playbar(gtx)
		}),
	)
	if w.settingsOpen {
		w.settingsDialog(gtx)
	}
	if w.wcOpen && w.st.Phase == "play" {
		w.wcDialog(gtx)
	}
	if st.Toast != "" && time.Now().Before(st.ToastUntil) {
		w.toast(gtx, st.Toast)
		gtx.Execute(op.InvalidateCmd{At: st.ToastUntil})
	}
}

// pageOf lays out blocks in a scrolling, centred column.
func (w *Window) pageOf(gtx C, blocks []layout.Widget) D {
	return material.List(w.th, &w.page).Layout(gtx, len(blocks), func(gtx C, i int) D {
		in := layout.Inset{Left: 24, Right: 24}
		if i == 0 {
			in.Top = 24
		}
		if i == len(blocks)-1 {
			in.Bottom = 32
		}
		return in.Layout(gtx, func(gtx C) D { return column(gtx, 860, blocks[i]) })
	})
}

func (w *Window) needsSetup() bool {
	s := w.st.Settings
	if s.SetupDone {
		w.inSetup = 2
	}
	if w.inSetup == 0 {
		w.inSetup = 2
		if !w.st.MGBAFound || (s.GamesFolder == "" && len(s.Recent) == 0) {
			w.inSetup = 1
		}
	}
	return w.inSetup == 1
}

// --------------------------------------------------------------------------------- clicks
func (w *Window) handleClicks(gtx C, setup bool) {
	a := w.app
	if w.b("settings").Clicked(gtx) {
		w.settingsOpen = true
	}
	if w.b("update").Clicked(gtx) || w.b("update-get").Clicked(gtx) {
		u := w.st.Update.URL
		if u == "" {
			u = releasesPage
		}
		openDefault(u)
	}
	if w.b("update-check").Clicked(gtx) && !w.st.Update.Checking {
		go a.checkUpdates(true)
	}
	w.wcClicks(gtx)
	for _, what := range []string{"rom", "folder", "mgba"} {
		for _, where := range []string{"", "-2", "-set"} {
			if w.b("browse-" + what + where).Clicked(gtx) {
				go a.browse(what)
			}
		}
	}
	if w.b("setup-done").Clicked(gtx) && w.st.MGBAFound {
		w.inSetup = 2
		go a.setSettings(func(s *Settings) { s.SetupDone = true })
	}
	if w.b("play").Clicked(gtx) && w.selected != "" {
		w.startPlay(w.selected)
	}
	if w.b("again").Clicked(gtx) && w.st.Game != nil && w.st.Mode != "backup" && w.st.Mode != "restore" {
		w.startPlay(w.st.Game.ROM)
	}
	if w.b("home").Clicked(gtx) {
		a.goHome()
	}
	if w.b("backup").Clicked(gtx) || w.b("backup-2").Clicked(gtx) || w.b("backup-again").Clicked(gtx) {
		go func() {
			if err := a.backup(); err != nil {
				a.ui.Toast(err.Error())
			}
		}()
	}
	if w.b("restore").Clicked(gtx) || w.b("restore-again").Clicked(gtx) {
		go func() {
			if err := a.restoreSave(); err != nil {
				a.ui.Toast(err.Error())
			}
		}()
	}
	if w.b("gbawc").Clicked(gtx) || w.b("gbawc-again").Clicked(gtx) {
		go func() {
			if err := a.gbaWonderCards(); err != nil {
				a.ui.Toast(err.Error())
			}
		}()
	}
	if w.b("gbawc-done").Clicked(gtx) {
		go a.stopGBAWonderCards()
	}
	if w.b("cancel-copy").Clicked(gtx) {
		go a.cancelBackup()
	}
	if w.b("show-backup").Clicked(gtx) && w.st.BackupPath != "" {
		showInFolder(w.st.BackupPath)
	}
	if w.b("play-backup").Clicked(gtx) && w.st.BackupPath != "" {
		w.startPlay(w.st.BackupPath)
	}
	if w.b("copy-log").Clicked(gtx) && len(w.st.Log) > 0 {
		gtx.Execute(clipboardWrite(strings.Join(w.st.Log, "\n")))
		a.ui.Toast("Log copied")
	}
	if w.b("load-dat").Clicked(gtx) {
		go func() {
			p, err := pickFile("Choose a No-Intro GBA DAT file", "DAT files (*.dat, *.xml, *.zip)", "*.dat;*.xml;*.zip", "")
			if err != nil || p == "" {
				return
			}
			d, err := loadDATFile(p)
			if err != nil {
				a.ui.Toast("Couldn't load it: " + err.Error())
				return
			}
			a.ui.Toast(fmt.Sprintf("Loaded: %d GBA dumps", len(d.Entries)))
			a.refreshLibrary()
		}()
	}
	if w.b("search-nointro").Clicked(gtx) {
		openDefault("https://datomatic.no-intro.org/index.php?page=search&s=64")
	}
	if w.b("copy-sha1").Clicked(gtx) && w.st.Verify != nil {
		gtx.Execute(clipboardWrite(w.st.Verify.SHA1))
		a.ui.Toast("SHA-1 copied")
	}
	if w.b("details").Clicked(gtx) {
		w.detailsOpen = !w.detailsOpen
	}
	if w.b("copy").Clicked(gtx) {
		gtx.Execute(clipboardWrite(w.st.Controller.ScriptPath))
		a.ui.Toast("Copied - paste it into mGBA's file box")
	}
	if w.b("show").Clicked(gtx) {
		showInFolder(w.st.Controller.ScriptPath)
	}
	if w.b("shot").Clicked(gtx) {
		go a.saveScreenshot()
	}
	if w.b("shot-show").Clicked(gtx) && w.st.Controller.LastShot != "" {
		showInFolder(w.st.Controller.LastShot)
	}
	if w.b("settings-done").Clicked(gtx) || w.b("scrim").Clicked(gtx) {
		w.settingsOpen = false
	}
	if w.setCtl.Update(gtx) {
		v := w.setCtl.Value
		go a.setSettings(func(s *Settings) { s.Controller = v })
	}
	if w.setAsk.Update(gtx) {
		v := w.setAsk.Value
		go a.setSettings(func(s *Settings) { s.AskEachSave = v })
	}
	if w.setSeed.Update(gtx) {
		v := w.setSeed.Value
		go a.setSettings(func(s *Settings) { s.KeepEmeraldSeed = !v })
	}
	if w.setClock.Update(gtx) {
		v := w.setClock.Value
		go a.setSettings(func(s *Settings) { s.PCClock = !v })
	}
	if w.b("live-show").Clicked(gtx) {
		w.liveOn = true
	}
	if w.b("live-hide").Clicked(gtx) {
		w.liveOn = false
	}
	if w.b("live-pop").Clicked(gtx) { // out of the window and into one of its own
		w.liveOn = false
		go a.openLivePop()
	}
	if !w.setClock.Pressed() {
		w.setClock.Value = !w.st.Settings.PCClock
	}
	if !w.setSeed.Pressed() {
		w.setSeed.Value = !w.st.Settings.KeepEmeraldSeed
	}
	if !w.setCtl.Pressed() {
		w.setCtl.Value = w.st.Settings.Controller
	}
	if !w.setAsk.Pressed() {
		w.setAsk.Value = w.st.Settings.AskEachSave
	}
	for {
		if _, ok := w.search.Update(gtx); !ok {
			break
		}
	}
	if p := w.st.Prompt; p != nil {
		for _, bt := range p.Buttons {
			if w.promptBtn(p.ID, bt.ID).Clicked(gtx) {
				go a.ui.Press(p.ID, bt.ID)
			}
		}
	}
	for rom, cl := range w.games {
		for {
			c, ok := cl.Update(gtx)
			if !ok {
				break
			}
			w.selected = rom
			if c.NumClicks >= 2 {
				w.startPlay(rom)
			}
		}
	}
}

func (w *Window) startPlay(rom string) {
	go func() {
		if err := w.app.play(rom); err != nil {
			w.app.ui.Toast(err.Error())
			if !w.app.settings().SetupDone || w.app.settings().MGBA == "" {
				w.settingsOpen = true
			}
		}
	}()
}

func (w *Window) promptBtn(id int, b string) *widget.Clickable {
	k := strings.Join([]string{itoa(id), b}, ":")
	c := w.prompt[k]
	if c == nil {
		c = new(widget.Clickable)
		w.prompt[k] = c
	}
	return c
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// -------------------------------------------------------------------------------- top bar
func (w *Window) topbar(gtx C) D {
	st := w.st
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 12, Bottom: 12, Left: 24, Right: 20}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				s := gtx.Dp(36)
				if w.logo.Size().X == 0 {
					return D{Size: image.Pt(s, s)}
				}
				sc := float32(s) / float32(w.logo.Size().X)
				defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sc, sc))).Push(gtx.Ops).Pop()
				defer clip.Rect{Max: w.logo.Size()}.Push(gtx.Ops).Pop()
				w.logo.Filter = paint.FilterLinear
				w.logo.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				return D{Size: image.Pt(s, s)}
			}),
			layout.Rigid(gap(12)),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, "GB-Link Companion") }),
							layout.Rigid(gap(6)),
							layout.Rigid(func(gtx C) D { return w.label(gtx, 12.5, font.Normal, w.pal.muted, "v"+appVersion) }))
					}),
					layout.Rigid(func(gtx C) D {
						if gtx.Constraints.Max.X < gtx.Dp(260) {
							return D{}
						}
						return w.labelMax(gtx, 12.5, font.Normal, w.pal.muted, "Play in mGBA with the save on your real cartridge", 1)
					}))
			}),
			layout.Rigid(func(gtx C) D { // a newer version on GitHub
				if !st.Update.Newer {
					return D{}
				}
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
					return w.button(gtx, w.b("update"), "primary", "Update to v"+st.Update.Latest, false, nil)
				})
			}),
			layout.Rigid(func(gtx C) D {
				home := st.Phase == "home"
				inUse := false
				for _, l := range st.Log {
					if strings.Contains(l, "GB-Link connected") {
						inUse = true
					}
				}
				var ok bool
				var txt string
				var state int // 0 neutral 1 ok 2 bad
				if home {
					ok = st.Adapter != ""
					txt = map[bool]string{true: "GB-Link connected", false: "GB-Link not plugged in"}[ok]
					state = map[bool]int{true: 1, false: 2}[ok]
				} else if inUse {
					txt, state = "GB-Link connected", 1
				} else {
					txt, state = "GB-Link", 0
				}
				return w.chip(gtx, state, txt)
			}),
			layout.Rigid(gap(8)),
			layout.Rigid(func(gtx C) D {
				if st.MGBAFound {
					return w.chip(gtx, 1, "mGBA ready")
				}
				return w.chip(gtx, 2, "mGBA not found")
			}),
			layout.Rigid(gap(6)),
			layout.Rigid(func(gtx C) D {
				cl := w.b("settings")
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					bg := color.NRGBA{}
					if cl.Hovered() {
						bg = w.pal.surface2
					}
					return w.box(gtx, bg, color.NRGBA{}, 10, 8, func(gtx C) D { return icon(gtx, icSettings, w.pal.muted, 20) })
				})
			}),
		)
	})
	call := m.Stop()
	fill(gtx, dims.Size, 0, w.pal.surface)
	st2 := op.Offset(image.Pt(0, dims.Size.Y-gtx.Dp(1))).Push(gtx.Ops)
	fill(gtx, image.Pt(dims.Size.X, gtx.Dp(1)), 0, w.pal.border)
	st2.Pop()
	call.Add(gtx.Ops)
	return dims
}

// disclaimer: a slim strip under the top bar on every screen.
func (w *Window) disclaimer(gtx C) D {
	m := op.Record(gtx.Ops)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	dims := layout.Inset{Top: 6, Bottom: 6, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		return w.centered(gtx, 12, font.Normal, w.pal.muted,
			"GB-Link Companion is an independent, unofficial program and is not associated with gblink.io.")
	})
	call := m.Stop()
	fill(gtx, dims.Size, 0, w.pal.surface2)
	st := op.Offset(image.Pt(0, dims.Size.Y-gtx.Dp(1))).Push(gtx.Ops)
	fill(gtx, image.Pt(dims.Size.X, gtx.Dp(1)), 0, w.pal.border)
	st.Pop()
	call.Add(gtx.Ops)
	return dims
}

func (w *Window) chip(gtx C, state int, txt string) D {
	dot, fg := w.pal.muted, w.pal.muted
	switch state {
	case 1:
		dot, fg = w.pal.ok, w.pal.text
	case 2:
		dot = w.pal.bad
	}
	return w.box(gtx, w.pal.surface2, color.NRGBA{}, 999, 0, func(gtx C) D {
		return layout.Inset{Top: 5, Bottom: 5, Left: 11, Right: 11}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(8)
					fill(gtx, image.Pt(s, s), s/2, dot)
					return D{Size: image.Pt(s, s)}
				}),
				layout.Rigid(gap(7)),
				layout.Rigid(func(gtx C) D { return w.label(gtx, 13, font.Normal, fg, txt) }))
		})
	})
}

// ----------------------------------------------------------------------------------- setup
func (w *Window) setupView() []layout.Widget {
	st := w.st
	step := func(n string, done bool, title, desc, state string, stateOK bool, btnID, btnText string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D {
				return w.box(gtx, w.pal.surface, w.pal.border, 14, 0, func(gtx C) D {
					return layout.Inset{Top: 18, Bottom: 18, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								s := gtx.Dp(32)
								bg, fg := w.pal.accentSoft, w.pal.accent
								if done {
									bg, fg = w.pal.okSoft, w.pal.ok
								}
								fill(gtx, image.Pt(s, s), s/2, bg)
								gtx.Constraints = layout.Exact(image.Pt(s, s))
								return layout.Center.Layout(gtx, func(gtx C) D { return w.label(gtx, 15, font.Bold, fg, n) })
							}),
							layout.Rigid(gap(16)),
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, title) }),
									layout.Rigid(gap(2)),
									layout.Rigid(func(gtx C) D { return w.label(gtx, 14.5, font.Normal, w.pal.muted, desc) }),
									layout.Rigid(gap(6)),
									layout.Rigid(func(gtx C) D {
										c := w.pal.warn
										if stateOK {
											c = w.pal.ok
										} else if btnID == "browse-folder" {
											c = w.pal.muted
										}
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx C) D {
												if !stateOK {
													return D{}
												}
												return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D { return icon(gtx, icCheck, c, 18) })
											}),
											layout.Flexed(1, func(gtx C) D { return w.label(gtx, 14.5, font.SemiBold, c, state) }))
									}),
									layout.Rigid(func(gtx C) D {
										if btnID == "" {
											return D{}
										}
										return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
											return w.button(gtx, w.b(btnID), "secondary", btnText, false, nil)
										})
									}))
							}))
					})
				})
			})
		}
	}
	folder := st.Settings.GamesFolder
	folderText := "Not chosen yet (you can also browse for a game later)."
	if folder != "" {
		folderText = folder
	}
	mg := "mGBA wasn't found. Press “Find mGBA…” and choose mGBA.exe."
	if runtime.GOOS == "linux" {
		mg = "mGBA wasn't found. Install it (your distribution's mgba-qt package, the Flatpak from Flathub, or the AppImage from mgba.io), or press “Find mGBA…”."
	}
	if st.MGBAFound {
		mg = st.Settings.MGBA
		if id, ok := strings.CutPrefix(mg, "flatpak:"); ok {
			mg = "mGBA Flatpak (" + id + ")"
		}
	}
	ad := "Not found yet - plug it in and it will show up here."
	if st.Adapter != "" {
		ad = "GB-Link found"
	}
	return []layout.Widget{
		func(gtx C) D { return w.label(gtx, 24, font.SemiBold, w.pal.text, "Welcome! Let's get set up") },
		func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return w.label(gtx, 16, font.Normal, w.pal.muted, "Three quick things, then you're ready to play.")
			})
		},
		step("1", st.Adapter != "", "Plug in your GB-Link", "Connect it to this PC with USB, and to your GBA with a Game Boy Color link cable.", ad, st.Adapter != "", "", ""),
		step("2", st.MGBAFound, "mGBA", "The emulator your game runs in.", mg, st.MGBAFound, "browse-mgba", "Find mGBA…"),
		step("3", folder != "", "Your games folder", "Where your .gba files are. Your games will be listed here so you can just click one.", folderText, folder != "", "browse-folder", "Choose folder…"),
		func(gtx C) D {
			return layout.Inset{Top: 22}.Layout(gtx, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					style := "primary"
					if !st.MGBAFound {
						gtx = gtx.Disabled()
						style = "secondary"
					}
					return w.button(gtx, w.b("setup-done"), style, "Continue", true, nil)
				})
			})
		},
	}
}

// ---------------------------------------------------------------------------- game list
func (w *Window) selectedGame() *GameView {
	for i := range w.st.Library {
		if w.st.Library[i].ROM == w.selected {
			return &w.st.Library[i]
		}
	}
	return nil
}

// filterGames splits the library into recent / other games matching the search, and keeps the
// selection on a game that's on screen. Runs before layout (the play bar shows the selection).
func (w *Window) filterGames() (rec, rest []GameView) {
	st := w.st
	q := strings.ToLower(strings.TrimSpace(w.search.Text()))
	recent := map[string]bool{}
	for _, r := range st.Settings.Recent {
		recent[strings.ToLower(r)] = true
	}
	match := func(g GameView) bool {
		return q == "" || strings.Contains(strings.ToLower(g.Name), q) || strings.Contains(strings.ToLower(g.Title), q) || strings.Contains(strings.ToLower(g.Code), q)
	}
	for _, g := range st.Library {
		if !match(g) {
			continue
		}
		if recent[strings.ToLower(g.ROM)] {
			rec = append(rec, g)
		} else {
			rest = append(rest, g)
		}
	}
	visible := false
	for _, g := range append(append([]GameView{}, rec...), rest...) {
		if g.ROM == w.selected {
			visible = true
		}
	}
	if w.selectedGame() == nil || !visible { // keep the selection on a game that's on screen
		switch {
		case len(rec) > 0:
			w.selected = rec[0].ROM
		case len(rest) > 0:
			w.selected = rest[0].ROM
		}
	}
	return rec, rest
}

func (w *Window) homeView(gtx C) D {
	st := w.st
	rec, rest := w.filterGames()

	var rows []layout.Widget
	rows = append(rows, w.homeHead)
	if st.LibError != "" {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
				return w.box(gtx, w.pal.warnSoft, color.NRGBA{}, 10, 12, func(gtx C) D {
					return w.label(gtx, 14.5, font.SemiBold, w.pal.warn, st.LibError)
				})
			})
		})
	}
	heading := func(t string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Top: 18, Bottom: 6, Left: 4}.Layout(gtx, func(gtx C) D {
				return w.label(gtx, 12.5, font.Bold, w.pal.muted, strings.ToUpper(t))
			})
		}
	}
	if len(rec) > 0 {
		rows = append(rows, heading("Recently played"))
		for _, g := range rec {
			rows = append(rows, w.gameRow(g))
		}
	}
	if len(rest) > 0 {
		t := "Your games"
		if len(rec) > 0 {
			t = "All games"
		}
		rows = append(rows, heading(t))
		for _, g := range rest {
			rows = append(rows, w.gameRow(g))
		}
	}
	if len(st.Library) == 0 {
		rows = append(rows, w.emptyLibrary)
	} else if len(rec)+len(rest) == 0 {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Top: 30}.Layout(gtx, func(gtx C) D {
				return w.centered(gtx, 15, font.Normal, w.pal.muted, "No games match “"+w.search.Text()+"”.")
			})
		})
	}
	return w.pageOf(gtx, rows)
}

func (w *Window) homeHead(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return w.label(gtx, 24, font.SemiBold, w.pal.text, "Choose a game") }),
				layout.Rigid(func(gtx C) D {
					return w.button(gtx, w.b("gbawc"), "ghost", "Wonder Cards on your GBA", false, icGamepad)
				}),
				layout.Rigid(gap(4)),
				layout.Rigid(func(gtx C) D {
					return w.button(gtx, w.b("restore"), "ghost", "Restore a save", false, icUpload)
				}),
				layout.Rigid(gap(4)),
				layout.Rigid(func(gtx C) D {
					return w.button(gtx, w.b("backup"), "ghost", "Back up a cartridge", false, icDownload)
				}))
		}),
		layout.Rigid(gap(14)),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return w.box(gtx, w.pal.surface, w.pal.border, 10, 0, func(gtx C) D {
						return layout.Inset{Left: 12, Right: 12, Top: 9, Bottom: 9}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return icon(gtx, icSearch, w.pal.muted, 18) }),
								layout.Rigid(gap(8)),
								layout.Flexed(1, func(gtx C) D {
									ed := material.Editor(w.th, &w.search, "Search your games")
									ed.Color, ed.HintColor = w.pal.text, w.pal.muted
									ed.Font.Typeface = w.face
									ed.TextSize = 14.5
									return ed.Layout(gtx)
								}))
						})
					})
				}),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D {
					return w.button(gtx, w.b("browse-rom"), "secondary", "Browse for a game…", false, icFolder)
				}))
		}),
	)
}

func (w *Window) gameRow(g GameView) layout.Widget {
	return func(gtx C) D {
		cl := w.games[g.ROM]
		if cl == nil {
			cl = new(widget.Clickable)
			w.games[g.ROM] = cl
		}
		sel := g.ROM == w.selected
		return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				bg, border := w.pal.surface, w.pal.border
				if sel {
					border = w.pal.accent
					bg = mix(w.pal.accentSoft, w.pal.surface, .55)
				} else if cl.Hovered() {
					border = mix(w.pal.accent, w.pal.border, .45)
				}
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return w.box(gtx, bg, border, 12, 0, func(gtx C) D {
					return layout.Inset{Top: 10, Bottom: 10, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.cartArt(gtx, &g, 40) }),
							layout.Rigid(gap(14)),
							layout.Flexed(1, func(gtx C) D {
								meta := []string{}
								if g.Title != "" && !strings.EqualFold(g.Title, g.Name) {
									meta = append(meta, g.Title)
								}
								if g.Code != "" {
									meta = append(meta, g.Code)
								}
								if g.Played != "" {
									meta = append(meta, "Last saved "+g.Played)
								} else {
									meta = append(meta, "No save on this PC yet")
								}
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 15, font.SemiBold, w.pal.text, g.Name, 1) }),
									layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 13, font.Normal, w.pal.muted, strings.Join(meta, " · "), 1) }))
							}),
							layout.Rigid(func(gtx C) D {
								if !g.Pending {
									return D{}
								}
								return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
									return w.badge(gtx, w.pal.warnSoft, w.pal.warn, nil, "Save not on cart yet")
								})
							}),
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return w.verifyBadge(gtx, g.Verify) })
							}))
					})
				})
			})
		})
	}
}

func (w *Window) emptyLibrary(gtx C) D {
	return layout.Inset{Top: 48}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return icon(gtx, icGamepad, w.pal.muted, 48) }),
			layout.Rigid(gap(8)),
			layout.Rigid(func(gtx C) D { return w.centered(gtx, 17, font.SemiBold, w.pal.text, "No games here yet") }),
			layout.Rigid(func(gtx C) D {
				return w.centered(gtx, 14.5, font.Normal, w.pal.muted, "Choose the folder with your .gba files, browse for one game, or back up a cartridge you own.")
			}),
			layout.Rigid(gap(14)),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return w.button(gtx, w.b("browse-folder-2"), "primary", "Choose games folder…", false, nil)
					}),
					layout.Rigid(gap(10)),
					layout.Rigid(func(gtx C) D {
						return w.button(gtx, w.b("browse-rom-2"), "secondary", "Browse for a game…", false, nil)
					}),
					layout.Rigid(gap(10)),
					layout.Rigid(func(gtx C) D {
						return w.button(gtx, w.b("backup-2"), "secondary", "Back up a cartridge", false, icDownload)
					}))
			}))
	})
}

func (w *Window) playbar(gtx C) D {
	g := w.selectedGame()
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 12, Bottom: 12, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		return column(gtx, 860, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.cartArt(gtx, g, 32) }),
				layout.Rigid(gap(14)),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.label(gtx, 13, font.Normal, w.pal.muted, "Selected") }),
						layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 16, font.SemiBold, w.pal.text, g.Name, 1) }),
						layout.Rigid(func(gtx C) D {
							t, c := "", w.pal.muted
							switch g.Verify {
							case "ok":
								t, c = "Verified good dump: "+g.NoIntro, w.pal.ok
							case "bad":
								t, c = "Can't verify checksum: it doesn't match No-Intro's dumps of this game (modified, or a bad dump?)", w.pal.warn
							case "unknown":
								t = "Not in No-Intro's list (a hack, homebrew or unlisted game?)"
							case "":
								t = "Checking against No-Intro…"
							}
							return w.labelMax(gtx, 12.5, font.Normal, c, t, 1)
						}))
				}),
				layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("play"), "primary", "Play", true, icPlay) }))
		})
	})
	call := m.Stop()
	fill(gtx, dims.Size, 0, w.pal.surface)
	fill(gtx, image.Pt(dims.Size.X, gtx.Dp(1)), 0, w.pal.border)
	call.Add(gtx.Ops)
	return dims
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x)*t + float64(y)*(1-t)) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

// ----------------------------------------------------------------------------- a session
func (w *Window) sessionView(gtx C) []layout.Widget {
	st := w.st
	phase := st.Phase
	if phase == "connect" {
		phase = "load"
	}
	doneAll := st.Phase == "done" && (st.Result == "done" || st.Result == "backup" || st.Result == "restore" || st.Result == "gbawc")
	eyebrow := map[string]string{"load": "Getting ready", "play": "Now playing", "finish": "Saving to the cartridge"}[phase]
	if st.Mode == "backup" {
		eyebrow = map[string]string{"load": "Backing up a cartridge", "dump": "Backing up"}[phase]
	}
	if st.Mode == "restore" {
		eyebrow = map[string]string{"load": "Restoring a save", "dump": "Restoring a save"}[phase]
	}
	if st.Mode == "gbawc" {
		eyebrow = "Wonder Cards on your GBA"
	}
	if phase == "done" {
		eyebrow = map[bool]string{true: "Finished", false: "Stopped"}[doneAll]
	}
	blocks := []layout.Widget{
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return w.cartArt(gtx, st.Game, 56) }),
				layout.Rigid(gap(16)),
				layout.Flexed(1, func(gtx C) D {
					name, sub := "", ""
					if st.Game != nil {
						name = st.Game.Name
						sub = strings.Join(nonEmpty(st.Game.Title, st.Game.Code), " · ")
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.label(gtx, 12.5, font.Bold, w.pal.accent, strings.ToUpper(eyebrow)) }),
						layout.Rigid(func(gtx C) D { return w.labelMax(gtx, 24, font.SemiBold, w.pal.text, name, 1) }),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 13, font.Normal, w.pal.muted, sub) }))
				}))
		},
		func(gtx C) D {
			return layout.Inset{Top: 22}.Layout(gtx, func(gtx C) D { return w.stepper(gtx, st.Mode, phase, doneAll) })
		},
	}
	if p := st.Prompt; p != nil {
		blocks = append(blocks, func(gtx C) D { return w.promptCard(gtx, p) })
	}
	if pr := st.Progress; pr != nil {
		blocks = append(blocks, func(gtx C) D { return w.progressCard(gtx, pr) })
	}
	if st.Prompt == nil && st.Progress == nil && (st.Phase == "connect" || st.Phase == "load" || st.Phase == "finish" || st.Phase == "dump") && st.Status != "" {
		blocks = append(blocks, func(gtx C) D {
			return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
				return w.card(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.spinner(gtx, 20) }),
						layout.Rigid(gap(12)),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.text, st.Status) }))
				})
			})
		})
	}
	if st.Phase == "play" && st.Mode == "gbawc" {
		blocks = append(blocks, w.gbaWCStatus, w.wcCard, func(gtx C) D {
			return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return w.centered(gtx, 13.5, font.Normal, w.pal.muted, "Play as usual, and save in the game to keep what the cards give you. The link cable stays in while you send cards.")
					}),
					layout.Rigid(gap(12)),
					layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("gbawc-done"), "secondary", "Done", false, nil) }))
			})
		})
	} else if st.Phase == "play" {
		blocks = append(blocks, w.tiles)
		blocks = append(blocks, w.wcCard)
		c := st.Controller
		if c.Enabled && c.ScriptConnected {
			if w.liveOn {
				blocks = append(blocks, w.liveCard)
			}
			blocks = append(blocks, w.shotCard)
		}
		if c.Enabled && c.Manual && !c.ScriptConnected {
			blocks = append(blocks, w.scriptCard)
		}
		if st.Prompt == nil {
			blocks = append(blocks, func(gtx C) D {
				return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
					return w.centered(gtx, 13.5, font.Normal, w.pal.muted, "Play and save as usual. When you're done, just close mGBA and your save will be copied to the cartridge.")
				})
			})
		}
	}
	if st.Phase == "done" {
		blocks = append(blocks, w.doneCard)
	}
	blocks = append(blocks, w.details)
	return blocks
}

func nonEmpty(s ...string) []string {
	var o []string
	for _, x := range s {
		if x != "" {
			o = append(o, x)
		}
	}
	return o
}

func (w *Window) stepper(gtx C, mode, phase string, doneAll bool) D {
	steps := []string{"Load save from cartridge", "Play in mGBA", "Save back to cartridge"}
	cur := map[string]int{"load": 0, "play": 1, "finish": 2}[phase]
	if mode == "backup" {
		steps = []string{"Read the cartridge", "Copy the game", "Saved on this PC"}
		cur = map[string]int{"load": 0, "dump": 1}[phase]
	}
	if mode == "restore" {
		steps = []string{"Check the cartridge", "Write the save", "Checked and done"}
		cur = map[string]int{"load": 0, "dump": 1}[phase]
	}
	if mode == "gbawc" {
		steps = []string{"Start Emerald on the GBA", "Walk around in the game", "Send Wonder Cards"}
		cur = map[string]int{"load": 0, "play": 1}[phase]
		if phase == "play" && (w.st.GBAWC == "ready" || w.st.GBAWC == "busy") {
			cur = 2
		}
	}
	if phase == "done" {
		cur = -1
	}
	var kids []layout.FlexChild
	for i, s := range steps {
		i, s := i, s
		if i > 0 {
			kids = append(kids, layout.Rigid(gap(8)))
		}
		kids = append(kids, layout.Flexed(1, func(gtx C) D {
			bg, fg, nbg, nfg := w.pal.surface2, w.pal.muted, w.pal.surface, w.pal.muted
			switch {
			case i == cur:
				bg, fg, nbg, nfg = w.pal.accentSoft, w.pal.accent, w.pal.accent, color.NRGBA{255, 255, 255, 255}
			case doneAll || (cur >= 0 && i < cur):
				fg, nbg, nfg = w.pal.ok, w.pal.ok, color.NRGBA{255, 255, 255, 255}
			}
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return w.box(gtx, bg, color.NRGBA{}, 10, 0, func(gtx C) D {
				return layout.Inset{Top: 10, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							sz := gtx.Dp(24)
							fill(gtx, image.Pt(sz, sz), sz/2, nbg)
							gtx.Constraints = layout.Exact(image.Pt(sz, sz))
							return layout.Center.Layout(gtx, func(gtx C) D { return w.label(gtx, 13, font.Bold, nfg, itoa(i+1)) })
						}),
						layout.Rigid(gap(10)),
						layout.Flexed(1, func(gtx C) D { return w.labelMax(gtx, 14, font.SemiBold, fg, s, 1) }))
				})
			})
		}))
	}
	return layout.Flex{}.Layout(gtx, kids...)
}

func (w *Window) promptCard(gtx C, p *Prompt) D {
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		border := w.pal.border
		titleColor := w.pal.text
		if p.Kind == "error" {
			border, titleColor = mix(w.pal.bad, w.pal.border, .4), w.pal.bad
		}
		return w.box(gtx, w.pal.surface, border, 14, 22, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					if p.Kind != "power" {
						return D{}
					}
					return layout.Inset{Right: 22}.Layout(gtx, w.gbaArt)
				}),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.label(gtx, 19, font.SemiBold, titleColor, p.Title) }),
						layout.Rigid(gap(6)),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.Normal, w.pal.text, p.Text) }),
						layout.Rigid(func(gtx C) D {
							if p.Kind != "power" {
								return D{}
							}
							return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return w.spinner(gtx, 18) }),
									layout.Rigid(gap(10)),
									layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.accent, "Waiting for the GBA…") }))
							})
						}),
						layout.Rigid(func(gtx C) D {
							var kids []layout.FlexChild
							for i, bt := range p.Buttons {
								bt := bt
								if i > 0 {
									kids = append(kids, layout.Rigid(gap(10)))
								}
								kids = append(kids, layout.Rigid(func(gtx C) D {
									return w.button(gtx, w.promptBtn(p.ID, bt.ID), bt.Style, bt.Label, p.Kind != "power", nil)
								}))
							}
							return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D { return layout.Flex{}.Layout(gtx, kids...) })
						}))
				}))
		})
	})
}

func (w *Window) progressCard(gtx C, pr *Progress) D {
	pct := min(100, pr.Done*100/max(1, pr.Total))
	note := "Keep the GBA switched on and the cable plugged in."
	if w.st.Writing {
		note = "Keep the GBA switched on and the cable plugged in until this finishes."
	}
	if pr.Detail != "" {
		note = pr.Detail
	}
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		return w.card(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.text, pr.Label) }),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.text, itoa(pct)+"%") }))
				}),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D {
					wd, h := gtx.Constraints.Max.X, gtx.Dp(12)
					fill(gtx, image.Pt(wd, h), h/2, w.pal.surface2)
					if pct > 0 {
						fill(gtx, image.Pt(max(h, wd*pct/100), h), h/2, w.pal.accent)
					}
					return D{Size: image.Pt(wd, h)}
				}),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D { return w.label(gtx, 13.5, font.Normal, w.pal.muted, note) }),
						layout.Rigid(func(gtx C) D {
							if !pr.Cancellable {
								return D{}
							}
							return w.button(gtx, w.b("cancel-copy"), "secondary", "Cancel", false, nil)
						}))
				}))
		})
	})
}

func (w *Window) spinner(gtx C, size unit.Dp) D {
	s := gtx.Dp(size)
	gtx.Constraints = layout.Exact(image.Pt(s, s))
	l := material.Loader(w.th)
	l.Color = w.pal.accent
	return l.Layout(gtx)
}

func (w *Window) tile(gtx C, tone string, ic *widget.Icon, title, txt string) D {
	ibg, ifg := w.pal.surface2, w.pal.muted
	switch tone {
	case "ok":
		ibg, ifg = w.pal.okSoft, w.pal.ok
	case "warn":
		ibg, ifg = w.pal.warnSoft, w.pal.warn
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return w.box(gtx, w.pal.surface, w.pal.border, 14, 16, func(gtx C) D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				s := gtx.Dp(34)
				fill(gtx, image.Pt(s, s), gtx.Dp(10), ibg)
				gtx.Constraints = layout.Exact(image.Pt(s, s))
				return layout.Center.Layout(gtx, func(gtx C) D { return icon(gtx, ic, ifg, 20) })
			}),
			layout.Rigid(gap(12)),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.text, title) }),
					layout.Rigid(func(gtx C) D { return w.label(gtx, 13.5, font.Normal, w.pal.muted, txt) }))
			}))
	})
}

func (w *Window) tiles(gtx C) D {
	st := w.st
	cart := func(gtx C) D {
		switch {
		case st.Writing:
			return w.tile(gtx, "", icSync, "Cartridge save", "Saving to the cartridge…")
		case st.Cart == "synced":
			t := "Up to date"
			if st.LastSync != "" {
				t += " · " + st.LastSync
			}
			return w.tile(gtx, "ok", icCheck, "Cartridge save", t)
		case st.Cart == "pending":
			return w.tile(gtx, "warn", icDots, "Cartridge save", "Your newest save is on this PC. It's copied over when the GBA is on.")
		}
		return w.tile(gtx, "", icDots, "Cartridge save", "Save in the game, and it's copied to the cartridge.")
	}
	gba := func(gtx C) D {
		switch st.GBA {
		case "on":
			return w.tile(gtx, "ok", icCheck, "GBA", "On and connected")
		case "off":
			return w.tile(gtx, "warn", icPower, "GBA", "Off. To reconnect, switch it on while holding START + SELECT.")
		}
		return w.tile(gtx, "", icCheck, "GBA", "Connected")
	}
	ctl := func(gtx C) D {
		c := st.Controller
		switch {
		case !c.Enabled:
			return w.tile(gtx, "", icRemove, "GBA as controller", "Off (turn it on in Settings)")
		case c.ScriptConnected && c.Online && c.GBAShows != "" && c.Passthru && c.GBAShows != "its menu":
			return w.tile(gtx, "ok", icGamepad, "GBA", "Showing "+c.GBAShows+", and its buttons control mGBA. Hold SELECT on the GBA to stop.")
		case c.ScriptConnected && c.Online && c.GBAShows == "its menu":
			return w.tile(gtx, "ok", icGamepad, "GBA", "Showing its menu. Choose “Use GBA as controller” to play, or pick a screen.")
		case c.ScriptConnected && c.Online && c.GBAShows != "":
			return w.tile(gtx, "ok", icGamepad, "GBA", "Showing "+c.GBAShows+". Hold SELECT on the GBA to play with it while it shows this.")
		case c.ScriptConnected && c.Online:
			return w.tile(gtx, "ok", icGamepad, "GBA as controller", "On: the GBA's buttons control mGBA. Hold SELECT for its status menu.")
		case c.ScriptConnected:
			return w.tile(gtx, "warn", icGamepad, "GBA as controller", "Paused while the GBA is off")
		case c.Manual:
			return w.tile(gtx, "warn", icGamepad, "GBA as controller", "One more step: load the script in mGBA (below)")
		}
		return w.tile(gtx, "", icGamepad, "GBA as controller", "Starting…")
	}
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		if gtx.Constraints.Max.X < gtx.Dp(640) {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(cart), layout.Rigid(gap(12)), layout.Rigid(gba), layout.Rigid(gap(12)), layout.Rigid(ctl))
		}
		// three equal tiles, all as tall as the tallest
		cw := (gtx.Constraints.Max.X - 2*gtx.Dp(12)) / 3
		ws := []layout.Widget{cart, gba, ctl}
		hmax := 0
		for _, wd := range ws { // measure, so all three are as tall as the tallest
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Constraints.Max.Y)}
			m := op.Record(gtx.Ops)
			hmax = max(hmax, wd(g).Size.Y)
			m.Stop()
		}
		for i, wd := range ws {
			st := op.Offset(image.Pt(i*(cw+gtx.Dp(12)), 0)).Push(gtx.Ops)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(cw, hmax))
			w.stretch(g, wd)
			st.Pop()
		}
		return D{Size: image.Pt(gtx.Constraints.Max.X, hmax)}
	})
}

// stretch lays out a tile so its background fills the exact constraints.
func (w *Window) stretch(gtx C, wd layout.Widget) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	return wd(gtx)
}

// shotCard: "Save screenshot" for the status screen the GBA shows now
func (w *Window) shotCard(gtx C) D {
	c := w.st.Controller
	text := "Save a picture of the status screen the GBA shows now (RNG state, wild Pokémon, trainer's team, map), or watch it here as it changes."
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		return w.card(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.label(gtx, 16, font.SemiBold, w.pal.text, "Screenshot") }),
						layout.Rigid(gap(4)),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 13.5, font.Normal, w.pal.muted, text) }))
				}),
				layout.Rigid(gap(16)),
				layout.Rigid(func(gtx C) D {
					if c.LastShot == "" {
						return D{}
					}
					return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D {
						return w.button(gtx, w.b("shot-show"), "ghost", "Show the file", false, nil)
					})
				}),
				layout.Rigid(func(gtx C) D {
					if w.liveOn {
						return D{}
					}
					return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D {
						return w.button(gtx, w.b("live-show"), "secondary", "Show GBA screen", false, nil)
					})
				}),
				layout.Rigid(func(gtx C) D {
					return w.button(gtx, w.b("shot"), "primary", "Save screenshot", false, nil)
				}))
		})
	})
}

func (w *Window) scriptCard(gtx C) D {
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		return w.box(gtx, w.pal.surface, mix(w.pal.accent, w.pal.border, .35), 14, 22, func(gtx C) D {
			step := func(n, t string) layout.FlexChild {
				return layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.accent, n+".  ") }),
							layout.Flexed(1, func(gtx C) D { return w.label(gtx, 15, font.Normal, w.pal.text, t) }))
					})
				})
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return w.label(gtx, 16, font.SemiBold, w.pal.text, "Turn on the GBA controller in mGBA")
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 6, Bottom: 4}.Layout(gtx, func(gtx C) D {
						return w.label(gtx, 15, font.Normal, w.pal.text, "Your mGBA can't do this by itself, so load the controller script once mGBA is open:")
					})
				}),
				step("1", "In mGBA, open Tools › Scripting…"),
				step("2", "Choose File › Load script…"),
				step("3", "Pick gblink_controller.lua (press “Copy its location”, then paste it into the file box)"),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("copy"), "secondary", "Copy its location", false, nil) }),
							layout.Rigid(gap(10)),
							layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("show"), "ghost", "Show the file", false, nil) }))
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
						return w.label(gtx, 13, font.Normal, w.pal.muted, "Tip: mGBA development builds (from mgba.io) load it automatically.")
					})
				}))
		})
	})
}

func (w *Window) doneCard(gtx C) D {
	st := w.st
	r := st.Result
	ibg, ifg, ic := w.pal.surface2, w.pal.muted, icRemove
	title, txt := "Stopped", st.ResultText
	switch r {
	case "done":
		ibg, ifg, ic, title = w.pal.okSoft, w.pal.ok, icCheck, "All done!"
	case "pending":
		ibg, ifg, ic, title = w.pal.warnSoft, w.pal.warn, icWarn, "Your save is on this PC"
	case "backup":
		ibg, ifg, ic, title = w.pal.okSoft, w.pal.ok, icCheck, "Backed up!"
	case "restore":
		ibg, ifg, ic, title = w.pal.okSoft, w.pal.ok, icCheck, "Save written!"
	case "failed":
		ibg, ifg, ic, title = w.pal.warnSoft, w.pal.warn, icWarn, "The backup didn't finish"
		if st.Mode == "gbawc" {
			title = "Wonder Cards stopped"
		} else if st.Mode == "restore" {
			title = "The save wasn't written"
		}
	case "gbawc":
		ibg, ifg, ic, title = w.pal.okSoft, w.pal.ok, icCheck, "Done"
	}
	type btn struct{ id, style, label string }
	buttons := []btn{{"again", "primary", "Play again"}, {"home", "secondary", "Choose another game"}}
	switch {
	case r == "backup":
		buttons = []btn{{"play-backup", "primary", "Play it now"}, {"show-backup", "secondary", "Show the file"}, {"home", "secondary", "Back to games"}}
	case st.Mode == "backup":
		buttons = []btn{{"backup-again", "primary", "Try again"}, {"home", "secondary", "Back to games"}}
	case st.Mode == "restore":
		buttons = []btn{{"restore-again", "primary", "Write another save"}, {"home", "secondary", "Back to games"}}
	case st.Mode == "gbawc":
		buttons = []btn{{"gbawc-again", "primary", "Start again"}, {"home", "secondary", "Back to games"}}
	}
	if txt == "" && r == "cancelled" {
		txt = "Nothing was changed on the cartridge."
		if st.Mode == "backup" {
			txt = "Nothing was saved."
		} else if st.Mode == "gbawc" {
			txt = "Nothing was sent to the GBA."
		}
	}
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D {
		return w.box(gtx, w.pal.surface, w.pal.border, 14, 30, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(64)
					fill(gtx, image.Pt(s, s), s/2, ibg)
					gtx.Constraints = layout.Exact(image.Pt(s, s))
					return layout.Center.Layout(gtx, func(gtx C) D { return icon(gtx, ic, ifg, 34) })
				}),
				layout.Rigid(gap(10)),
				layout.Rigid(func(gtx C) D { return w.centered(gtx, 19, font.SemiBold, w.pal.text, title) }),
				layout.Rigid(gap(4)),
				layout.Rigid(func(gtx C) D { return w.centered(gtx, 15, font.Normal, w.pal.text, txt) }),
				layout.Rigid(func(gtx C) D {
					if st.Verify == nil || r != "backup" {
						return D{}
					}
					return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D { return w.verifyBox(gtx, st.Verify) })
				}),
				layout.Rigid(gap(16)),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = 0
					var kids []layout.FlexChild
					for i, b := range buttons {
						b := b
						if i > 0 {
							kids = append(kids, layout.Rigid(gap(10)))
						}
						kids = append(kids, layout.Rigid(func(gtx C) D { return w.button(gtx, w.b(b.id), b.style, b.label, true, nil) }))
					}
					return layout.Flex{}.Layout(gtx, kids...)
				}))
		})
	})
}

func (w *Window) details(gtx C) D {
	return layout.Inset{Top: 22}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						cl := w.b("details")
						return cl.Layout(gtx, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							ic := icExpand
							if w.detailsOpen {
								ic = icCollapse
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return icon(gtx, ic, w.pal.muted, 18) }),
								layout.Rigid(gap(4)),
								layout.Rigid(func(gtx C) D { return w.label(gtx, 13, font.SemiBold, w.pal.muted, "Details") }))
						})
					}),
					layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
					layout.Rigid(func(gtx C) D {
						if len(w.st.Log) == 0 {
							return D{}
						}
						return w.button(gtx, w.b("copy-log"), "ghost", "Copy log", false, icCopy)
					}))
			}),
			layout.Rigid(func(gtx C) D {
				if !w.detailsOpen {
					return D{}
				}
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return w.box(gtx, w.pal.surface2, color.NRGBA{}, 10, 12, func(gtx C) D {
						l := material.Label(w.th, 12.5, strings.Join(w.st.Log, "\n"))
						l.Font.Typeface = "Go Mono"
						l.Color = w.pal.muted
						return l.Layout(gtx)
					})
				})
			}))
	})
}

// gbaArt: a photo of a GBA, with rings pulsing around START and SELECT.
func (w *Window) gbaArt(gtx C) D {
	if w.gbaPhoto.Size().X == 0 {
		return D{}
	}
	ps := w.gbaPhoto.Size()
	wd := gtx.Dp(250)
	sc := float32(wd) / float32(ps.X)
	ht := int(float32(ps.Y) * sc)
	st := op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(sc, sc))).Push(gtx.Ops)
	cl := clip.Rect{Max: ps}.Push(gtx.Ops)
	w.gbaPhoto.Filter = paint.FilterLinear
	w.gbaPhoto.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	st.Pop()

	// START (upper) and SELECT (lower), in the photo's pixels
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	a := uint8(90 + 165*(0.5+0.5*math.Cos(2*math.Pi*t)))
	acc := rgb(0xffb020) // amber: stands out on any colour of GBA
	ring := func(px, py float32) {
		cx, cy := int(px*sc), int(py*sc)
		r, th := gtx.Dp(9), gtx.Dp(3)
		off := op.Offset(image.Pt(cx-r, cy-r)).Push(gtx.Ops)
		paint.FillShape(gtx.Ops, color.NRGBA{acc.R, acc.G, acc.B, a},
			clip.Stroke{Path: clip.Ellipse{Max: image.Pt(2*r, 2*r)}.Path(gtx.Ops), Width: float32(th)}.Op())
		off.Pop()
	}
	ring(85.5, 148.8)
	ring(79.5, 172.5)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(50 * time.Millisecond)})
	return D{Size: image.Pt(wd, ht)}
}

// ------------------------------------------------------------------------------ settings
func (w *Window) settingsDialog(gtx C) {
	// scrim: clicking outside closes
	w.b("scrim").Layout(gtx, func(gtx C) D {
		fill(gtx, gtx.Constraints.Max, 0, w.pal.scrim)
		return D{Size: gtx.Constraints.Max}
	})
	st := w.st
	row := func(name, desc, val string, right layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 14, Bottom: 14}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, w.pal.text, name) }),
							layout.Rigid(func(gtx C) D {
								if desc == "" {
									return D{}
								}
								return w.label(gtx, 13, font.Normal, w.pal.muted, desc)
							}),
							layout.Rigid(func(gtx C) D {
								if val == "" {
									return D{}
								}
								return w.label(gtx, 13, font.Normal, w.pal.muted, val)
							}))
					}),
					layout.Rigid(gap(16)),
					layout.Rigid(right))
			})
		})
	}

	sw := func(b *widget.Bool) layout.Widget {
		return func(gtx C) D {
			return b.Layout(gtx, func(gtx C) D {
				width := gtx.Dp(36)
				height := gtx.Dp(20)
				thumbSize := gtx.Dp(16)
				inset := gtx.Dp(2)

				size := image.Pt(width, height)
				trackColor := w.pal.border
				thumbColor := w.pal.muted
				thumbX := inset

				if b.Value {
					trackColor = w.pal.accent
					thumbColor = w.pal.bg
					thumbX = width - thumbSize - inset
				}

				fill(gtx, size, height/2, trackColor)

				defer op.Offset(image.Pt(thumbX, inset)).Push(gtx.Ops).Pop()
				fill(
					gtx,
					image.Pt(thumbSize, thumbSize),
					thumbSize/2,
					thumbColor,
				)

				return D{Size: size}
			})
		}
	}

	div := layout.Rigid(func(gtx C) D {
		fill(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Dp(1)), 0, w.pal.border)
		return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))}
	})
	mg, fo := st.Settings.MGBA, st.Settings.GamesFolder
	if mg == "" {
		mg = "Not found"
	}
	if fo == "" {
		fo = "Not chosen"
	}
	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X-gtx.Dp(32), gtx.Dp(580))
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		// stop clicks inside the dialog reaching the scrim
		var d D
		dialog := func(gtx C) D {
			d = w.box(gtx, w.pal.surface, w.pal.border, 16, 24, func(gtx C) D {
				rows := func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						row("mGBA", "", mg, func(gtx C) D { return w.button(gtx, w.b("browse-mgba-set"), "secondary", "Change…", false, nil) }),
						div,
						row("Games folder", "", fo, func(gtx C) D { return w.button(gtx, w.b("browse-folder-set"), "secondary", "Change…", false, nil) }),
						div,
						row("Use the GBA as a controller", "The GBA's buttons control the game in mGBA while you play. Its menu (hold SELECT) also shows Pokémon RNG, wild and trainer info, and the map.", "", sw(&w.setCtl)),
						div,
						row("Use the cartridge's clock", "Pokémon Ruby, Sapphire and Emerald: mGBA's clock starts from your cartridge's clock, so berries, tides and daily events carry on the same when you go back to the GBA. Turn it off to use this PC's clock.", "", sw(&w.setClock)),
						div,
						row("Fix Emerald's starting seed", "Pokémon Emerald has a bug: its RNG starts from the same seed every time. With this on, the game gets a random seed when it starts in mGBA, the way it was meant to. Turn it off if your RNG manipulation relies on the old seed.", "", sw(&w.setSeed)),
						div,
						row("Ask me straight away", "If you save while the GBA is off, ask to switch it on right then, instead of when mGBA closes.", "", sw(&w.setAsk)),
						div,
						row("Version "+appVersion, "Updates come from github.com/Shinygati/GB-Link-Companion (Releases). It checks when it starts.", st.Update.Text,
							func(gtx C) D {
								switch {
								case st.Update.Newer:
									return w.button(gtx, w.b("update-get"), "primary", "Download v"+st.Update.Latest, false, nil)
								case st.Update.Checking:
									return w.button(gtx, w.b("update-check"), "secondary", "Checking…", false, nil)
								}
								return w.button(gtx, w.b("update-check"), "secondary", "Check for updates", false, nil)
							}),
						div,
						row("About", "GB-Link Companion is an independent, unofficial program. It isn't made, endorsed or supported by the makers of the GB-Link adapter (gblink.io).", "",
							func(gtx C) D { return D{} }),
						div,
						row("No-Intro database", "Used to check that backups and games are good dumps. For a newer one, download the GBA DAT from datomatic.no-intro.org.", st.DATInfo,
							func(gtx C) D { return w.button(gtx, w.b("load-dat"), "secondary", "Load a DAT file…", false, nil) }))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 19, font.SemiBold, w.pal.text, "Settings") }),
					layout.Flexed(1, func(gtx C) D { // scrolls when the window is short
						return w.setList.List.Layout(gtx, 1, func(gtx C, _ int) D { return rows(gtx) }) // scrolls, no scroll bar
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
							return layout.E.Layout(gtx, func(gtx C) D { return w.button(gtx, w.b("settings-done"), "primary", "Done", false, nil) })
						})
					}))
			})
			return d
		}
		// a click inside the dialog (but not on a control) mustn't close it
		return w.b("dialog-catch").Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.Y -= gtx.Dp(32)
			gtx.Constraints.Min.Y = min(gtx.Constraints.Max.Y, gtx.Dp(760))
			gtx.Constraints.Max.Y = gtx.Constraints.Min.Y
			return dialog(gtx)
		})
	})
}

func (w *Window) toast(gtx C, t string) {
	layout.S.Layout(gtx, func(gtx C) D {
		return layout.Inset{Bottom: 96}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return w.box(gtx, w.pal.text, color.NRGBA{}, 10, 0, func(gtx C) D {
				return layout.Inset{Top: 10, Bottom: 10, Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
					return w.label(gtx, 14.5, font.SemiBold, w.pal.bg, t)
				})
			})
		})
	})
}

func clipboardWrite(t string) clipboard.WriteCmd {
	return clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(t))}
}

func (w *Window) badge(gtx C, bg, fg color.NRGBA, ic *widget.Icon, t string) D {
	return w.box(gtx, bg, color.NRGBA{}, 999, 0, func(gtx C) D {
		return layout.Inset{Top: 3, Bottom: 3, Left: 9, Right: 9}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					if ic == nil {
						return D{}
					}
					return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return icon(gtx, ic, fg, 14) })
				}),
				layout.Rigid(func(gtx C) D { return w.label(gtx, 12, font.SemiBold, fg, t) }))
		})
	})
}

// verifyBadge: the No-Intro check of a game in the list.
func (w *Window) verifyBadge(gtx C, status string) D {
	switch status {
	case "ok":
		return w.badge(gtx, w.pal.okSoft, w.pal.ok, icCheck, "Verified")
	case "bad":
		return w.badge(gtx, w.pal.warnSoft, w.pal.warn, nil, "Can't verify checksum")
	case "unknown":
		return w.badge(gtx, w.pal.surface2, w.pal.muted, nil, "Not in No-Intro")
	}
	return w.badge(gtx, w.pal.surface2, w.pal.muted, nil, "Checking…")
}

// verifyBox: the No-Intro check of a backup, on the "Backed up!" card.
func (w *Window) verifyBox(gtx C, v *Verify) D {
	bg, fg, ic := w.pal.surface2, w.pal.muted, icRemove
	switch v.Status {
	case "ok":
		bg, fg, ic = w.pal.okSoft, w.pal.ok, icCheck
	case "bad":
		bg, fg, ic = w.pal.warnSoft, w.pal.warn, icWarn
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return w.box(gtx, bg, color.NRGBA{}, 12, 16, func(gtx C) D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return icon(gtx, ic, fg, 22) }),
			layout.Rigid(gap(12)),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return w.label(gtx, 15, font.SemiBold, fg, v.Title) }),
					layout.Rigid(gap(2)),
					layout.Rigid(func(gtx C) D { return w.label(gtx, 14, font.Normal, w.pal.text, v.Text) }),
					layout.Rigid(gap(6)),
					layout.Rigid(func(gtx C) D {
						return w.label(gtx, 12.5, font.Normal, w.pal.muted, "SHA-1 "+v.SHA1+" · CRC32 "+v.CRC)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
							return layout.Flex{}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("copy-sha1"), "secondary", "Copy SHA-1", false, icCopy) }),
								layout.Rigid(gap(8)),
								layout.Rigid(func(gtx C) D {
									return w.button(gtx, w.b("search-nointro"), "ghost", "Search No-Intro…", false, icSearch)
								}))
						})
					}))
			}))
	})
}

// gbaWCStatus: how Emerald on the GBA is doing (Wonder Cards on your GBA, wcgba.go).
func (w *Window) gbaWCStatus(gtx C) D {
	tone, title, txt := "", "Starting Emerald…", "Continue your game from the title screen and walk around: then the cards can be sent."
	switch w.st.GBAWC {
	case "ready":
		tone, title, txt = "ok", "Ready", "You're walking around in Emerald: send a card whenever you like."
	case "busy":
		tone, title, txt = "ok", "Ready (you're busy in the game)", "A card you use now waits until you can move again; one you add goes in straight away."
	case "away":
		tone, title, txt = "warn", "Not now", "The GBA isn't answering: the link cable is out, or you're in a battle, a menu or another screen. "+
			"Plug the cable back in and walk around in the game: it picks up again by itself."
	case "nolink":
		tone, title, txt = "warn", "GB-Link unplugged", "Plug the GB-Link adapter back in: it picks up again by itself. Emerald keeps running on the GBA."
	}
	return layout.Inset{Top: 18}.Layout(gtx, func(gtx C) D { return w.tile(gtx, tone, icGamepad, title, txt) })
}
