// Status screens on the GBA while you play: a menu on the GBA picks what it shows.
//
//   - Use GBA as controller: the GBA's buttons control mGBA (hold SELECT for the menu again)
//   - RNG state, Wild Pokemon, Trainer's team, Map location: live info from the Pokemon game in
//     mGBA, read by the mGBA script (gblink_controller.lua) and sent here as text screens.
//
// The GBA only draws (CMD_STATUS in the helper); the menus run here, from the buttons the bridge
// already reads from the GBA ~100 times a second.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// modeNow: the GBA's mode for other goroutines (the bridge's statusState belongs to pollLoop).
type modeBox struct{ v atomic.Int32 }

func (b *Bridge) currentMode() int { return int(b.modeNow.v.Load()) }

// GBA modes. The numbers 1-4 are also what the mGBA script calls its screens.
const (
	modeController = 0
	modeRNG        = 1
	modeWild       = 2
	modeTrainer    = 3
	modeMap        = 4
	modeMenu       = 5
	modeEncounters = 6 // made here, from the script's E lines
)

var modeKeys = map[int]string{modeController: "controller", modeRNG: "rng", modeWild: "wild", modeTrainer: "trainer", modeMap: "map", modeMenu: "menu", modeEncounters: "encounters"}

func modeFromKey(k string) int {
	for m, s := range modeKeys {
		if s == k {
			return m
		}
	}
	return modeMenu
}

// What the GBA shows, for the window.
var modeTitles = map[int]string{modeController: "", modeRNG: "RNG state", modeWild: "Wild Pokémon",
	modeTrainer: "Trainer's team", modeMap: "Map location", modeMenu: "its menu", modeEncounters: "the encounter tracker"}

type menuItem struct {
	mode  int
	label string
}

var menuItems = []menuItem{
	{modeController, "Use GBA as controller"},
	{modeRNG, "RNG state"},
	{modeWild, "Wild Pokemon"},
	{modeTrainer, "Trainer's team"},
	{modeMap, "Map location"},
	{modeEncounters, "Encounter tracker"},
}

// items: the menu for this game (no RNG screen for your own builds)
func (b *Bridge) items() []menuItem {
	b.mu.Lock()
	no := b.noRNG
	b.mu.Unlock()
	if !no {
		return menuItems
	}
	var out []menuItem
	for _, it := range menuItems {
		if it.mode != modeRNG {
			out = append(out, it)
		}
	}
	return out
}

// GBA buttons (KEYINPUT bits, 1 = pressed)
const (
	kA, kB, kSelect, kStart      = 0x001, 0x002, 0x004, 0x008
	kRight, kLeft, kUp, kDown    = 0x010, 0x020, 0x040, 0x080
	kR, kL                       = 0x100, 0x200
	stCols, stRows               = 40, 17
	stBodyRows                   = 15 // rows 1-15 scroll; row 0 is the title, row 16 the hints
	colWhite, colGrey, colGreen  = 0, 1, 2
	colRed, colYellow, colCyan   = 3, 4, 5
	stOpClear, stOpRows, stOpMap = 1, 2, 3
	stOpMark, stOpLeave          = 4, 5
	stOpSprite, stOpImage        = 6, 7
	stOpImage8, stOpRestore      = 8, 9
	stSprites                    = 8 // picture slots on the GBA (ST_SPRITES)
	stRowWords                   = 16
	mapCell, mapTop              = 8, 32
	cmdStatus                    = 12
	statusEvery                  = 150 * time.Millisecond // fastest the GBA's screen follows the game
	markBlink                    = 450 * time.Millisecond
)

type stLine struct {
	color byte
	text  string
}

type stMap struct {
	w, h, px, py int
	cells        []byte
}

// stSprite: a picture on a screen, at text column col, its top on the screen's line `line`
// (1 = the title line); data = 16 colours then 4bpp tiles, as the GBA wants them
type stSprite struct {
	slot, col, line, w, h int
	data                  []byte
}

// stPic: a picture of a place, one colour (BGR555) per dot
type stPic struct {
	w, h   int
	colors []uint16
}

// stBg: the game's region map picture (indexed: colours, then a byte a pixel)
type stBg struct {
	w, h   int
	colors []uint16
	pixels []byte
}

// screen: what the mGBA script sent for one mode
type screen struct {
	lines   []stLine
	grid    *stMap
	sprites []stSprite
	picture *stPic
	bg      string            // the region map picture to show under the map (see Bridge.bgs)
	icons   map[string][]byte // the game's map icons: hero, cursor (16 colours + 4 tiles)
}

// where a picture is on the GBA's screen
type placed struct {
	x, y, w, h int
	hash       uint64
	over       bool // drawn over the kept background picture
}

// statusState is the bridge's side of the status screens. Only pollLoop touches it, except the
// script's screens (under Bridge.mu).
type statusState struct {
	mode      int
	menuSel   int
	scroll    int
	prevKeys  int
	held      time.Time // when the current buttons were first held (for repeats and the combo)
	repeatAt  time.Time
	suppress  bool      // ignore buttons until they're all let go (after switching mode)
	passthru  bool      // on a status screen, the GBA's buttons control mGBA too (hold SELECT)
	selDown   time.Time // when SELECT was pressed on its own (zero: it isn't)
	selDone   bool      // this SELECT hold already toggled
	selTap    time.Time // a short SELECT press, passed on to mGBA until then
	supported bool      // the helper on the GBA knows CMD_STATUS
	unknown   bool      // not checked yet
	onGBA     int       // what the GBA shows now: -1 nothing known (redraw everything), else a mode
	onGBAMap  bool      // ...and whether that's laid out with a map
	rows      [stRows][stCols]uint16
	mapHash   uint64
	mark      [3]int // x, y, on
	lastSend  time.Time
	lastBlink time.Time
	blinkOn   bool
	lastShown int // the last screen chosen (where the menu's cursor starts)
	sprites   map[int]placed
	// the map screen's cursor: A on the map shows it, A again a picture of the place under it
	cur        [2]int
	curOn      bool
	preview    bool
	cmark      [3]int // the cursor as the GBA shows it
	wantPic    *stPic // set by compose: the picture to show, or nil
	wantBase   string // set by compose: the PokeNav page under everything ("" none)
	wantBg     string // ...and a picture on top of it (the region map), at wantBgY
	wantBgY    int
	bgShown    string // the region map picture on the GBA's screen now ("" none)
	bgKept     string // ...and the one the GBA keeps (survives clearing the screen)
	encEdit    bool   // setting the encounter count
	encVal     int
	encDigit   int // 0 = ones
	encSession int // wild Pokemon seen since the app started this game
	onGBAPic   bool
	picHash    uint64
}

// ------------------------------------------------------------------------- the mGBA script
// readScript reads the lines the mGBA script sends: H (which game), D (a screen).
func (b *Bridge) readScript(c net.Conn) {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		switch line[0] {
		case 'H':
			f := strings.Split(line[1:], "\t")
			name := f[0]
			noRNG := false
			for _, x := range f[1:] {
				if x == "-rng" { // your own builds: no RNG screen
					noRNG = true
				}
				if x == "wc" { // it can take Wonder Cards
					b.mu.Lock()
					b.scriptWC = true
					b.mu.Unlock()
				}
			}
			b.mu.Lock()
			b.noRNG = noRNG
			b.mu.Unlock()
			b.mu.Lock()
			if strings.HasPrefix(name, "!") {
				b.game, b.gameProblem = "", name[1:]
			} else {
				b.game, b.gameProblem = name, ""
			}
			b.mu.Unlock()
			if strings.HasPrefix(name, "!") {
				b.s.ui.Log("Status screens: %s", name[1:])
			} else {
				b.s.ui.Log("Status screens: reading Pokémon %s in mGBA.", titleCase(name))
			}
		case 'w': // a Wonder Card's result (wondercards.go)
			b.wcReply(line)
		case 'E': // a wild Pokemon appeared: species, level, shiny
			f := strings.Split(line[1:], "\t")
			if len(f) == 3 {
				b.mu.Lock()
				b.encNew++
				b.encLast = f[0] + " Lv" + f[1]
				b.encShiny = f[2] == "1"
				b.mu.Unlock()
			}
		case 'B':
			f := strings.SplitN(line[1:], "\t", 2)
			if len(f) == 2 {
				if bg := parseBg(f[1]); bg != nil {
					b.mu.Lock()
					b.bgs[f[0]] = bg
					b.mu.Unlock()
				}
			}
		case 'D':
			parts := strings.Split(line[1:], "\t")
			m, err := strconv.Atoi(parts[0])
			if err != nil || m < modeRNG || m > modeMap {
				continue
			}
			scr := &screen{}
			for _, p := range parts[1:] {
				if p == "" {
					continue
				}
				if p[0] == 'G' {
					scr.grid = parseGrid(p[1:])
					continue
				}
				if p[0] == 'P' {
					scr.picture = parsePic(p[1:])
					continue
				}
				if p[0] == 'R' {
					scr.bg = p[1:]
					continue
				}
				if p[0] == 'I' {
					if f := strings.SplitN(p[1:], ",", 2); len(f) == 2 {
						if d, err := hex.DecodeString(f[1]); err == nil && len(d) == 32+128 {
							if scr.icons == nil {
								scr.icons = map[string][]byte{}
							}
							scr.icons[f[0]] = d
						}
					}
					continue
				}
				if p[0] == 'S' {
					if sp := parseSprite(p[1:]); sp != nil {
						scr.sprites = append(scr.sprites, *sp)
					}
					continue
				}
				col := p[0] - '0'
				if col > 5 {
					col = 0
				}
				scr.lines = append(scr.lines, stLine{col, p[1:]})
			}
			b.mu.Lock()
			b.screens[m] = scr
			b.mu.Unlock()
		}
	}
}

// titleCase: "LEAFGREEN v1.1 QoL" -> "LeafGreen v1.1 QoL" for the log
func titleCase(s string) string {
	names := map[string]string{"RUBY": "Ruby", "SAPPHIRE": "Sapphire", "EMERALD": "Emerald", "FIRERED": "FireRed", "LEAFGREEN": "LeafGreen"}
	w := strings.Fields(s)
	if len(w) > 0 && names[w[0]] != "" {
		w[0] = names[w[0]]
	}
	return strings.Join(w, " ")
}

func parseGrid(s string) *stMap {
	f := strings.SplitN(s, ",", 5)
	if len(f) != 5 {
		return nil
	}
	n := make([]int, 4)
	for i := 0; i < 4; i++ {
		v, err := strconv.Atoi(f[i])
		if err != nil {
			return nil
		}
		n[i] = v
	}
	g := &stMap{w: n[0], h: n[1], px: n[2], py: n[3]}
	if g.w <= 0 || g.h <= 0 || g.w*mapCell > 240 || g.w*g.h > 28*15 || len(f[4]) != g.w*g.h {
		return nil
	}
	g.cells = make([]byte, len(f[4]))
	for i := 0; i < len(f[4]); i++ {
		c := f[4][i] - '0'
		if c > 4 {
			c = 0
		}
		g.cells[i] = c
	}
	return g
}

func parseBg(s string) *stBg {
	f := strings.SplitN(s, ",", 4)
	if len(f) != 4 {
		return nil
	}
	w, err1 := strconv.Atoi(f[0])
	h, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 || w > 240 || h > 160 || len(f[2]) != 4*256 || len(f[3]) != 2*w*h {
		return nil
	}
	bg := &stBg{w: w, h: h, colors: make([]uint16, 256)}
	for i := range bg.colors {
		v, err := strconv.ParseUint(f[2][4*i:4*i+4], 16, 16)
		if err != nil {
			return nil
		}
		bg.colors[i] = uint16(v)
	}
	px, err := hex.DecodeString(f[3])
	if err != nil {
		return nil
	}
	bg.pixels = px
	return bg
}

func parsePic(s string) *stPic {
	f := strings.SplitN(s, ",", 3)
	if len(f) != 3 {
		return nil
	}
	w, err1 := strconv.Atoi(f[0])
	h, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 || w > 240 || h > 128 || len(f[2]) != 4*w*h {
		return nil
	}
	pic := &stPic{w: w, h: h, colors: make([]uint16, w*h)}
	for i := range pic.colors {
		v, err := strconv.ParseUint(f[2][4*i:4*i+4], 16, 16)
		if err != nil {
			return nil
		}
		pic.colors[i] = uint16(v)
	}
	return pic
}

func parseSprite(s string) *stSprite {
	f := strings.SplitN(s, ",", 6)
	if len(f) != 6 {
		return nil
	}
	n := make([]int, 5)
	for i := 0; i < 5; i++ {
		v, err := strconv.Atoi(f[i])
		if err != nil {
			return nil
		}
		n[i] = v
	}
	sp := &stSprite{slot: n[0], col: n[1], line: n[2], w: n[3], h: n[4]}
	if sp.slot < 0 || sp.slot >= 8 || sp.w <= 0 || sp.h <= 0 || sp.w%8 != 0 || sp.h%8 != 0 || sp.w > 64 || sp.h > 64 {
		return nil
	}
	data, err := hex.DecodeString(f[5])
	if err != nil || len(data) != 32+sp.w*sp.h/2 {
		return nil
	}
	sp.data = data
	return sp
}

// tell the script which screen to make (it only reads the game for the one that's showing)
func (b *Bridge) sendMode(m int) {
	lua := 0
	if m >= modeRNG && m <= modeMap {
		lua = m
	}
	b.writeAll([]byte("M" + strconv.Itoa(lua) + "\n"))
}

// ------------------------------------------------------------------------------ buttons
// statusKeys handles the GBA's buttons outside controller mode. Returns true if the screen
// should be redrawn now.
func (b *Bridge) statusKeys(keys int) bool {
	st := &b.st
	now := time.Now()
	if keys != st.prevKeys {
		st.held, st.repeatAt = now, now.Add(350*time.Millisecond)
	}
	pressed := keys &^ st.prevKeys
	if keys == st.prevKeys && keys != 0 && now.After(st.repeatAt) { // held: repeat Up/Down/L/R
		pressed |= keys & (kUp | kDown | kL | kR)
		st.repeatAt = now.Add(90 * time.Millisecond)
	}
	st.prevKeys = keys
	if st.suppress {
		if keys == 0 {
			st.suppress = false
		}
		return false
	}
	if pressed == 0 {
		return false
	}
	if st.mode == modeMenu {
		items := b.items()
		st.menuSel = min(st.menuSel, len(items)-1)
		switch {
		case pressed&kUp != 0:
			st.menuSel = (st.menuSel + len(items) - 1) % len(items)
		case pressed&kDown != 0:
			st.menuSel = (st.menuSel + 1) % len(items)
		case pressed&(kA|kStart) != 0:
			b.setMode(items[st.menuSel].mode)
		default:
			return false
		}
		return true
	}
	if st.mode == modeMap && b.mapKeys(pressed) {
		return true
	}
	if st.mode == modeEncounters && b.encounterKeys(pressed) {
		return true
	}
	switch {
	case pressed&kB != 0:
		b.setMode(modeMenu)
	case pressed&kUp != 0:
		st.scroll--
	case pressed&kDown != 0:
		st.scroll++
	case pressed&kL != 0:
		st.scroll -= stBodyRows - 1
	case pressed&kR != 0:
		st.scroll += stBodyRows - 1
	case pressed&(kLeft|kRight) != 0 && st.mode != modeController: // next / previous screen
		d := 1
		if pressed&kLeft != 0 {
			d = -1
		}
		items := b.items()[1:] // the screens, without "Use GBA as controller"
		i := 0
		for k, it := range items {
			if it.mode == st.mode {
				i = k
			}
		}
		b.setMode(items[(i+d+len(items))%len(items)].mode)
	default:
		return false
	}
	return true
}

// letGoKeys: mGBA lets go of the GBA's buttons now (a save is about to be written to the cart,
// and the GBA can't send its buttons meanwhile), and ignores them until they're all released.
func (b *Bridge) letGoKeys() {
	b.send(0, true)
	b.letGo.Store(true)
}

// encounterKeys: the encounter counter. START starts or stops counting; A sets the count, digit
// by digit (Left/Right pick a digit, Up/Down change it, A sets it, B leaves it as it was).
func (b *Bridge) encounterKeys(pressed int) bool {
	st := &b.st
	gs := &b.s.state
	if !st.encEdit {
		switch {
		case pressed&kStart != 0:
			gs.EncTracking = !gs.EncTracking
			b.s.game.saveState(*gs)
		case pressed&kA != 0:
			st.encEdit, st.encVal, st.encDigit = true, gs.EncCount, 0
		default:
			return false
		}
		return true
	}
	pow := 1
	for i := 0; i < st.encDigit; i++ {
		pow *= 10
	}
	digit := st.encVal / pow % 10
	switch {
	case pressed&kLeft != 0:
		st.encDigit = min(st.encDigit+1, 5)
	case pressed&kRight != 0:
		st.encDigit = max(st.encDigit-1, 0)
	case pressed&kUp != 0:
		st.encVal += ((digit+1)%10 - digit) * pow
	case pressed&kDown != 0:
		st.encVal += ((digit+9)%10 - digit) * pow
	case pressed&kA != 0:
		gs.EncCount, st.encEdit = st.encVal, false
		b.s.game.saveState(*gs)
		b.s.ui.Log("Encounter count set to %d.", gs.EncCount)
	case pressed&kB != 0:
		st.encEdit = false
	}
	return true
}

// countEncounters adds the wild Pokemon the script saw since last time (when counting is on).
func (b *Bridge) countEncounters() {
	b.mu.Lock()
	n := b.encNew
	b.encNew = 0
	b.mu.Unlock()
	if n == 0 {
		return
	}
	gs := &b.s.state
	b.st.encSession += n
	if gs.EncTracking {
		gs.EncCount += n
		b.s.game.saveState(*gs)
	}
}

// mapKeys: the map screen's cursor. A shows it on your spot; the D-pad moves it from place to
// place; A shows a picture of the place under it; B goes back a step. Returns true if handled.
func (b *Bridge) mapKeys(pressed int) bool {
	st := &b.st
	b.mu.Lock()
	scr := b.screens[modeMap]
	b.mu.Unlock()
	if scr == nil || scr.grid == nil {
		return false
	}
	g := scr.grid
	filled := func(x, y int) bool { return x >= 0 && y >= 0 && x < g.w && y < g.h && g.cells[y*g.w+x] != 0 }
	switch {
	case !st.curOn && pressed&kA != 0:
		st.curOn, st.preview = true, false
		st.cur = [2]int{g.px, g.py}
		if !filled(g.px, g.py) { // not on the map: start at the first place
			for i, c := range g.cells {
				if c != 0 {
					st.cur = [2]int{i % g.w, i / g.w}
					break
				}
			}
		}
	case !st.curOn:
		return false
	case pressed&kB != 0 && st.preview:
		st.preview = false
	case pressed&kB != 0:
		st.curOn = false
	case pressed&kA != 0:
		st.preview = filled(st.cur[0], st.cur[1])
	case pressed&(kUp|kDown|kLeft|kRight) != 0:
		dx, dy := 0, 0
		switch {
		case pressed&kUp != 0:
			dy = -1
		case pressed&kDown != 0:
			dy = 1
		case pressed&kLeft != 0:
			dx = -1
		default:
			dx = 1
		}
		// to the next place that way: straight on, else the nearest one a little to the side
		best, bestD := [2]int{-1, -1}, 1<<30
		for step := 1; step < 32; step++ {
			for side := -step; side <= step; side++ {
				x, y := st.cur[0]+dx*step+dy*side, st.cur[1]+dy*step+dx*side
				if filled(x, y) {
					if d := step*step*4 + side*side; d < bestD {
						best, bestD = [2]int{x, y}, d
					}
				}
			}
			if best[0] >= 0 {
				break
			}
		}
		if best[0] < 0 {
			return true
		}
		st.cur = best
	default:
		return true // other buttons do nothing while looking around
	}
	b.sendCursor()
	return true
}

func (b *Bridge) sendCursor() {
	st := &b.st
	if !st.curOn {
		b.writeAll([]byte("C-\n"))
		return
	}
	v := 0
	if st.preview {
		v = 1
	}
	b.writeAll([]byte(fmt.Sprintf("C%d,%d,%d\n", st.cur[0], st.cur[1], v)))
}

// selectHold: on a status screen, holding SELECT on its own for half a second turns "the GBA's
// buttons control mGBA" on or off, so you can play and watch the screen. A short SELECT press
// still reaches the game (when it's on). Returns true when it toggled.
func (b *Bridge) selectHold(keys int) bool {
	st := &b.st
	now := time.Now()
	if keys == kSelect {
		if st.selDown.IsZero() {
			st.selDown, st.selDone = now, false
		}
		if !st.selDone && now.Sub(st.selDown) > 500*time.Millisecond {
			st.selDone = true
			st.passthru = !st.passthru
			st.suppress = !st.passthru // turning it off: ignore buttons until they're let go
			st.prevKeys = keys
			on := st.passthru
			b.s.ui.SetController(func(c *ControllerView) { c.Passthru = on })
			if on {
				b.s.ui.Log("The GBA's buttons control mGBA while it shows %s (hold SELECT to stop).", modeTitles[st.mode])
			}
			return true
		}
		return false
	}
	if !st.selDown.IsZero() && !st.selDone && keys == 0 && st.passthru {
		st.selTap = now.Add(100 * time.Millisecond) // a quick SELECT: the game gets it
	}
	if keys&kSelect == 0 {
		st.selDown = time.Time{}
	}
	return false
}

// controllerSelect: in controller mode, holding SELECT on its own for half a second opens the
// menu. SELECT is held back from mGBA while it might be that hold; a quick press reaches the game
// when it's let go. Returns whether to open the menu, and the buttons for mGBA.
func (b *Bridge) controllerSelect(keys int) (bool, int) {
	st := &b.st
	now := time.Now()
	if keys == kSelect {
		if st.selDown.IsZero() {
			st.selDown = now
		}
		if now.Sub(st.selDown) > 500*time.Millisecond {
			st.selDown = time.Time{}
			return true, 0
		}
		return false, 0
	}
	if !st.selDown.IsZero() && keys == 0 {
		st.selTap = now.Add(100 * time.Millisecond) // a quick SELECT: the game gets it
	}
	if keys&kSelect == 0 || keys != kSelect {
		st.selDown = time.Time{}
	}
	if keys == 0 && now.Before(st.selTap) {
		return false, kSelect
	}
	return false, keys
}

func (b *Bridge) setMode(m int) {
	st := &b.st
	if m == st.mode {
		return
	}
	st.mode, st.scroll, st.suppress = m, 0, true
	b.modeNow.v.Store(int32(m))
	if st.curOn {
		st.curOn, st.preview = false, false
		b.sendCursor()
	}
	if m == modeMenu {
		st.menuSel = 0
		for i, it := range b.items() { // the cursor on the screen we came from
			if it.mode == b.st.lastShown {
				st.menuSel = i
			}
		}
	} else {
		b.st.lastShown = m
	}
	b.sendMode(m)
	b.send(0, false)
	b.s.app.setStatusMode(modeKeys[m])
	b.s.ui.SetController(func(c *ControllerView) { c.GBAShows = modeTitles[m] })
}

// --------------------------------------------------------------------------- the screens
type cellText [stRows][stCols]uint16 // char | colour << 8

func putText(t *cellText, row, col int, color byte, s string) {
	if row < 0 || row >= stRows {
		return
	}
	for i := 0; i < len(s) && col+i < stCols; i++ {
		c := s[i]
		if c < 32 || c > 126 {
			c = '?'
		}
		if col+i >= 0 {
			t[row][col+i] = uint16(c) | uint16(color)<<8
		}
	}
}

func blankText() cellText {
	var t cellText
	for r := range t {
		for c := range t[r] {
			t[r][c] = ' '
		}
	}
	return t
}

func commaInt(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

// wrap splits text into lines of at most n characters.
func wrap(s string, n int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > n {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// compose builds what the GBA should show for the current mode: text, and a map or nil.
func (b *Bridge) compose() (cellText, *stMap, map[int]placed, map[int][]byte) {
	st := &b.st
	t := blankText()
	b.mu.Lock()
	game, problem, connected := b.game, b.gameProblem, len(b.clients) > 0
	scr := b.screens[st.mode]
	b.mu.Unlock()
	right := func(row int, color byte, s string) { putText(&t, row, stCols-len(s), color, s) }

	st.wantBg, st.wantBgY, st.wantBase = "", 0, ""
	b.mu.Lock()
	_, nav := b.bgs["POKENAV"]
	_, page := b.bgs["PAGE"]
	b.mu.Unlock()
	nav, page = false, false         // the PokeNav look is off: drawing it made each screen too slow
	if page && st.mode != modeMenu { // every screen on the PokeNav's page
		st.wantBase = "PAGE"
	}
	if st.mode == modeMenu {
		left := 1
		if nav { // the PokeNav's own screen, the options beside the device (no device in FRLG)
			st.wantBase = "POKENAV"
			if !strings.HasPrefix(game, "FIRERED") && !strings.HasPrefix(game, "LEAFGREEN") {
				left = 10
			}
		}
		putText(&t, 0, 0, colCyan, "GB-LINK STATUS")
		right(0, colCyan, game)
		items := b.items()
		st.menuSel = min(st.menuSel, len(items)-1)
		for i, it := range items {
			if i == st.menuSel {
				putText(&t, 2+i*2, left, colYellow, "> "+it.label)
			} else {
				putText(&t, 2+i*2, left+2, colWhite, it.label)
			}
		}
		row := 12
		switch {
		case !connected:
			putText(&t, row, 0, colRed, "mGBA's script isn't connected yet.")
		case problem != "":
			for i, l := range wrap(problem, stCols) {
				if i < 3 {
					putText(&t, row+i, 0, colRed, l)
				}
			}
		}
		putText(&t, 15, 0, colGrey, "Up/Down + A: choose. In a screen: B back")
		putText(&t, 16, 0, colGrey, "Controller: hold SELECT for the menu")
		return t, nil, nil, nil
	}

	if st.mode == modeEncounters {
		b.mu.Lock()
		last, shiny := b.encLast, b.encShiny
		b.mu.Unlock()
		gs := b.s.state
		putText(&t, 0, 0, colCyan, "ENCOUNTER TRACKER")
		right(0, colCyan, game)
		putText(&t, 2, 0, colWhite, "Encounters")
		putText(&t, 2, 16, colYellow, commaInt(gs.EncCount))
		if gs.EncTracking {
			putText(&t, 3, 0, colGreen, "Counting every wild Pokemon")
		} else {
			putText(&t, 3, 0, colGrey, "Not counting - START to begin")
		}
		putText(&t, 5, 0, colWhite, "This session")
		putText(&t, 5, 16, colWhite, commaInt(st.encSession))
		switch {
		case last == "":
			putText(&t, 6, 0, colGrey, "No wild Pokemon yet")
		case shiny:
			putText(&t, 6, 0, colYellow, "Last  "+last+"  SHINY!")
		default:
			putText(&t, 6, 0, colWhite, "Last  "+last)
		}
		if st.encEdit {
			digits := fmt.Sprintf("%06d", st.encVal)
			putText(&t, 9, 0, colCyan, "Set the count:")
			putText(&t, 9, 16, colWhite, digits)
			putText(&t, 10, 16+5-st.encDigit, colYellow, "^")
			putText(&t, 15, 0, colGrey, "<>: digit  ^v: change  A: set  B: cancel")
		} else {
			putText(&t, 15, 0, colGrey, "START: count on/off   A: set the count")
		}
		if st.passthru {
			putText(&t, 16, 0, colYellow, padRight("Playing in mGBA - hold SELECT to stop", 37))
		} else if !st.encEdit {
			putText(&t, 16, 0, colGrey, "B:menu  <>:screens  hold SELECT:play")
		}
		if !connected {
			putText(&t, 12, 0, colRed, "mGBA's script isn't connected yet.")
		}
		return t, nil, nil, nil
	}
	if scr == nil || len(scr.lines) == 0 {
		putText(&t, 0, 0, colCyan, strings.ToUpper(modeTitles[st.mode]))
		right(0, colCyan, game)
		switch {
		case !connected:
			putText(&t, 2, 0, colRed, "mGBA's script isn't connected yet.")
			putText(&t, 3, 0, colGrey, "See the GB-Link Companion window.")
		case problem != "":
			for i, l := range wrap(problem, stCols) {
				putText(&t, 2+i, 0, colRed, l)
			}
		default:
			putText(&t, 2, 0, colGrey, "Waiting for mGBA...")
		}
		putText(&t, 16, 0, colGrey, "B: menu")
		return t, nil, nil, nil
	}

	putText(&t, 0, 0, scr.lines[0].color, scr.lines[0].text)
	body := scr.lines[1:]
	if st.mode == modeMap {
		for i := 0; i < len(body) && i < 2; i++ {
			putText(&t, 1+i, 0, body[i].color, body[i].text)
		}
		st.wantPic = nil
		switch {
		case st.passthru:
			right(2, colYellow, " playing")
		case st.curOn && st.preview:
			right(1, colGrey, "  B:back")
			if scr.picture != nil {
				st.wantPic = scr.picture
			}
		case st.curOn:
			right(1, colGrey, "  A:pick B:back")
		default:
			right(2, colGrey, " A:look")
		}
		// the game's own map: its picture, your icon, and the cursor
		var want map[int]placed
		var data map[int][]byte
		b.mu.Lock()
		_, haveBg := b.bgs[scr.bg]
		b.mu.Unlock()
		if scr.bg != "" && haveBg && scr.grid != nil && st.wantPic == nil {
			st.wantBg, st.wantBgY = scr.bg, mapTop
			want, data = map[int]placed{}, map[int][]byte{}
			x0 := (240 - scr.grid.w*mapCell) / 2
			at := func(slot int, name string, cx, cy int) {
				d := scr.icons[name]
				if d == nil || cx < 0 || cy < 0 || cx >= scr.grid.w || cy >= scr.grid.h {
					return
				}
				x := min(max(x0+cx*mapCell-4, 0), 240-16)
				y := min(max(mapTop+cy*mapCell-4, 0), 160-16)
				hh := fnv.New64a()
				hh.Write(d)
				want[slot], data[slot] = placed{x, y, 16, 16, hh.Sum64(), true}, d
			}
			at(7, "hero", scr.grid.px, scr.grid.py)
			if st.curOn {
				at(6, "cursor", st.cur[0], st.cur[1])
			}
		}
		return t, scr.grid, want, data
	}
	maxScroll := max(0, len(body)-stBodyRows)
	st.scroll = max(0, min(st.scroll, maxScroll))
	for i := 0; i < stBodyRows && st.scroll+i < len(body); i++ {
		l := body[st.scroll+i]
		putText(&t, 1+i, 0, l.color, l.text)
	}
	hint := "B:menu  <>:screens  hold SELECT:play"
	if maxScroll > 0 {
		hint = "B:menu ^v:scroll  hold SELECT:play"
		more := ""
		if st.scroll > 0 {
			more += "^"
		}
		if st.scroll < maxScroll {
			more += "v"
		}
		right(16, colYellow, more)
	}
	if st.passthru {
		putText(&t, 16, 0, colYellow, padRight("Playing in mGBA - hold SELECT to stop", 37))
	} else {
		putText(&t, 16, 0, colGrey, hint)
	}
	// pictures, scrolled with their lines; only where they fit between the title and the hints
	want, data := map[int]placed{}, map[int][]byte{}
	for _, sp := range scr.sprites {
		row := 0
		if sp.line > 1 {
			row = 1 + (sp.line - 2) - st.scroll
		}
		x, y := sp.col*6, 3+row*9
		if (sp.line > 1 && row < 1) || y+sp.h > 3+16*9 || x+sp.w > 240 {
			continue
		}
		h := fnv.New64a()
		h.Write(sp.data)
		want[sp.slot] = placed{x, y, sp.w, sp.h, h.Sum64(), st.wantBase != ""}
		data[sp.slot] = sp.data
	}
	return t, nil, want, data
}

// ------------------------------------------------------------------------ to the GBA
func (h *Helper) Status(op int, extra []uint32, data []uint32) error {
	args := append([]uint32{uint32(op), uint32(len(data))}, extra...)
	st, _, err := h.Command(cmdStatus, args, data, 3*time.Second, []int{0}, time.Second)
	if err != nil {
		return err
	}
	if st != stOK {
		return &statusErr{st}
	}
	return nil
}

type statusErr struct{ st int }

func (e *statusErr) Error() string {
	if e.st < len(stNames) {
		return "the GBA said: " + stNames[e.st]
	}
	return "the GBA said no"
}

func rowWords(row int, cells [stCols]uint16) []uint32 {
	w := make([]uint32, stRowWords)
	w[0] = uint32(row)
	for c := 0; c < stCols; c++ {
		w[1+c/4] |= uint32(cells[c]&0xFF) << (8 * (c & 3))
		w[11+c/8] |= uint32(cells[c]>>8&7) << (4 * (c & 7))
	}
	return w
}

func mapHash(g *stMap) uint64 {
	h := fnv.New64a()
	var hdr [8]byte
	binary.LittleEndian.PutUint16(hdr[0:], uint16(g.w))
	binary.LittleEndian.PutUint16(hdr[2:], uint16(g.h))
	h.Write(hdr[:])
	h.Write(g.cells)
	return h.Sum64()
}

// drawStatus brings the GBA's screen up to date with compose(). Called with linkMu held.
func (b *Bridge) drawStatus(force bool) error {
	st := &b.st
	h := b.s.helper
	t, grid, want, spriteData := b.compose()
	pic := st.wantPic
	if st.mode != modeMap {
		pic = nil
	}
	if st.onGBA != st.mode || st.onGBAMap != (grid != nil) || st.onGBAPic != (pic != nil) {
		if err := h.Status(stOpClear, nil, nil); err != nil {
			return err
		}
		st.onGBA, st.onGBAMap, st.mapHash, st.mark = st.mode, grid != nil, 0, [3]int{-1, -1, 0}
		st.onGBAPic, st.picHash, st.cmark = pic != nil, 0, [3]int{-1, -1, 0}
		st.sprites = map[int]placed{}
		st.bgShown = ""
		for r := range st.rows {
			for c := range st.rows[r] {
				st.rows[r][c] = ' '
			}
		}
	}
	usingBg := false
	var layers []*stBg
	var ys []int
	key := ""
	b.mu.Lock()
	if bg := b.bgs[st.wantBase]; bg != nil {
		layers, ys, key = append(layers, bg), append(ys, 0), st.wantBase
	}
	if bg := b.bgs[st.wantBg]; bg != nil && st.wantBg != "" {
		layers, ys, key = append(layers, bg), append(ys, st.wantBgY), key+"|"+st.wantBg
		usingBg = true
	}
	b.mu.Unlock()
	whole := len(layers) > 0 && ys[0] == 0 && layers[0].h == 160 // the GBA keeps the whole screen
	if len(layers) > 0 && st.bgShown != key {
		sent := false
		if whole && st.bgKept == key { // the GBA still has it: just put it back
			sent = h.Status(stOpRestore, []uint32{0, 0, 240, 160}, nil) == nil
		}
		for i := 0; !sent && i < len(layers); i++ {
			bg := layers[i]
			words := make([]uint32, 128+(len(bg.pixels)+3)/4)
			for i, c := range bg.colors {
				words[i/2] |= uint32(c) << (16 * (i & 1))
			}
			for i, p := range bg.pixels {
				words[128+i/4] |= uint32(p) << (8 * (i & 3))
			}
			if err := h.Status(stOpImage8, []uint32{0, uint32(ys[i]), uint32(bg.w), uint32(bg.h), 256}, words); err != nil {
				return err
			}
		}
		st.bgShown, st.bgKept = key, key
		for slot := range st.sprites { // the picture covered them
			if st.sprites[slot].over {
				delete(st.sprites, slot)
			}
		}
		for r := range st.rows { // and the text on it: send it again
			st.rows[r][0] = 0xFFFF
		}
	}
	var data []uint32
	limit := stRows
	if grid != nil {
		limit = 3 // the map covers the rest
	}
	for r := 0; r < limit; r++ {
		if t[r] != st.rows[r] {
			data = append(data, rowWords(r, t[r])...)
		}
	}
	if len(data) > 0 {
		if err := h.Status(stOpRows, nil, data); err != nil {
			return err
		}
		for r := 0; r < limit; r++ {
			st.rows[r] = t[r]
		}
	}
	// Pictures over the region map (your icon, the cursor) can overlap, and the GBA takes one
	// away by putting the map back under it, which would rub out the other one too. So when any
	// of them changes, take them all away (the top one first) and draw them all again.
	overDirty := false
	for slot, p := range want {
		if p.over && st.sprites[slot] != p {
			overDirty = true
		}
	}
	for slot, p := range st.sprites {
		if _, ok := want[slot]; !ok && p.over {
			overDirty = true
		}
	}
	if overDirty {
		for slot := 0; slot < stSprites; slot++ { // the lower slot is drawn on top
			if p, ok := st.sprites[slot]; ok && p.over {
				if err := h.Status(stOpSprite, []uint32{uint32(slot), 0, 0, 0, 0}, nil); err != nil {
					return err
				}
				delete(st.sprites, slot)
			}
		}
	}
	for slot, p := range st.sprites { // pictures that went away
		if _, ok := want[slot]; !ok {
			if err := h.Status(stOpSprite, []uint32{uint32(slot), 0, 0, 0, 0}, nil); err != nil {
				return err
			}
			delete(st.sprites, slot)
			_ = p
		}
	}
	for slot := stSprites - 1; slot >= 0; slot-- { // new or changed pictures, the top one last
		p, ok := want[slot]
		if !ok || st.sprites[slot] == p {
			continue
		}
		d := spriteData[slot]
		words := make([]uint32, len(d)/4)
		for i := range words {
			words[i] = binary.LittleEndian.Uint32(d[4*i:])
		}
		flags := uint32(0)
		if p.over {
			flags = 1
		}
		if err := h.Status(stOpSprite, []uint32{uint32(slot), uint32(p.x), uint32(p.y), uint32(p.w), uint32(p.h), flags}, words); err != nil {
			return err
		}
		st.sprites[slot] = p
	}
	if pic != nil { // a picture of a place instead of the region map
		hh := fnv.New64a()
		for _, c := range pic.colors {
			hh.Write([]byte{byte(c), byte(c >> 8)})
		}
		if sum := hh.Sum64(); sum != st.picHash {
			words := make([]uint32, (len(pic.colors)+1)/2)
			for i, c := range pic.colors {
				words[i/2] |= uint32(c) << (16 * (i & 1))
			}
			if err := h.Status(stOpImage, []uint32{uint32(pic.w), uint32(pic.h), 0, mapTop, 240, 160 - mapTop}, words); err != nil {
				return err
			}
			st.picHash = sum
		}
		return nil
	}
	if grid != nil && !usingBg {
		if hh := mapHash(grid); hh != st.mapHash {
			cells := make([]uint32, (len(grid.cells)+3)/4)
			for i, c := range grid.cells {
				cells[i/4] |= uint32(c) << (8 * (i & 3))
			}
			x0 := (240 - grid.w*mapCell) / 2
			if err := h.Status(stOpMap, []uint32{uint32(grid.w), uint32(grid.h), uint32(x0), mapTop}, cells); err != nil {
				return err
			}
			st.mapHash = hh
			st.mark, st.cmark = [3]int{-1, -1, 0}, [3]int{-1, -1, 0}
		}
		cm := [3]int{255, 255, 0}
		if st.curOn {
			cm = [3]int{st.cur[0], st.cur[1], 1}
		}
		if cm != st.cmark {
			if err := h.Status(stOpMark, []uint32{uint32(cm[0]), uint32(cm[1]), uint32(cm[2]), 1}, nil); err != nil {
				return err
			}
			st.cmark = cm
		}
		on := 0
		if st.blinkOn {
			on = 1
		}
		if grid.px < grid.w && grid.py < grid.h && (st.mark != [3]int{grid.px, grid.py, on}) {
			if err := h.Status(stOpMark, []uint32{uint32(grid.px), uint32(grid.py), uint32(on), 0}, nil); err != nil {
				return err
			}
			st.mark = [3]int{grid.px, grid.py, on}
		}
	}
	return nil
}

// statusTick runs once per poll of the GBA's buttons (keys = buttons held). Called with linkMu
// held, with the helper answering. Returns the buttons to pass on to mGBA.
func (b *Bridge) statusTick(keys int, show bool) int {
	st := &b.st
	b.countEncounters()
	if st.unknown {
		st.unknown = false
		st.supported = b.s.helper.Status(stOpClear, nil, nil) == nil
		if !st.supported {
			b.s.ui.Log("This GBA helper can't show status screens; the GBA works as a controller only.")
			st.mode = modeController
		} else {
			st.onGBA = -1
		}
		show = true
	}
	if !st.supported || st.mode == modeController {
		fwd := keys
		if st.supported {
			menu, f := b.controllerSelect(keys)
			if menu {
				b.setMode(modeMenu)
				b.drawStatus(true)
				return 0
			}
			fwd = f
		}
		if show || st.onGBA != modeController {
			if b.s.helper.ShowControllerHint(st.supported) == nil {
				st.onGBA = modeController
				b.mu.Lock()
				b.needShow = false
				b.mu.Unlock()
			}
		}
		if st.suppress {
			if keys == 0 {
				st.suppress = false
			}
			return 0
		}
		return fwd
	}
	if show {
		st.onGBA = -1 // the GBA's screen was replaced (helper restarted, save written): draw it all
		st.bgKept = ""
		b.mu.Lock()
		b.needShow = false
		b.mu.Unlock()
	}
	if st.mode == modeRNG && b.items()[1].mode != modeRNG { // this game has no RNG screen
		b.setMode(modeMenu)
		st.suppress = false
	}
	now := time.Now()
	changed, fwd := false, 0
	if st.mode != modeMenu && b.selectHold(keys) {
		changed = true
	}
	if st.passthru && st.mode != modeMenu {
		fwd = keys
		if keys == kSelect { // maybe a hold to turn it off: keep it back (a quick press is sent on release)
			fwd = 0
		}
		if keys == 0 && now.Before(st.selTap) {
			fwd = kSelect
		}
		st.prevKeys = keys
	} else if !changed {
		changed = b.statusKeys(keys)
	}
	if st.mode == modeController { // just chose it
		b.s.helper.ShowControllerHint(true)
		st.onGBA = modeController
		return 0
	}
	if now.Sub(st.lastBlink) > markBlink {
		st.blinkOn, st.lastBlink = !st.blinkOn, now
	}
	if changed || st.onGBA != st.mode || now.Sub(st.lastSend) > statusEvery {
		t0 := time.Now()
		if err := b.drawStatus(false); err != nil {
			st.onGBA = -1
			debugf("drawStatus error: %v", err)
		}
		if d := time.Since(t0); d > 80*time.Millisecond {
			debugf("drawStatus took %v (mode %d)", d, st.mode)
		}
		st.lastSend = now
	}
	return fwd
}

func (h *Helper) ShowControllerHint(hint bool) error {
	a := uint32(0)
	if hint {
		a = 1
	}
	_, _, err := h.Command(cmdShow, []uint32{msgControl, a}, nil, 3*time.Second, []int{0}, time.Second)
	return err
}

func debugf(f string, a ...any) {
	if os.Getenv("GBLINK_DEBUG") != "" {
		fmt.Printf(time.Now().Format("15:04:05.000")+" debug: "+f+"\n", a...)
	}
}

// setStatusMode remembers the GBA's mode for next time (without reloading the game list).
func (a *App) setStatusMode(m string) {
	a.mu.Lock()
	a.set.StatusMode = m
	a.set.save()
	a.mu.Unlock()
}
