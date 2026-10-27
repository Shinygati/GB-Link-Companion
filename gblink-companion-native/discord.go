// Discord alerts for shiny wild Pokémon.
//
// When mGBA's script sees a shiny wild Pokémon it waits until the Pokémon is fully on screen, takes
// a picture of the game, and sends the app a "Y" line (status.go) together with the wild Pokémon's
// info screen. This file turns that into a Discord message: the text from the settings
// ({pokemon} {level} {game} {encounters}) with the two pictures attached.
//
// It talks to Discord's HTTP API directly, so there's nothing to install: either a bot (token +
// channel ID; the bot needs View Channel, Send Messages and Attach Files there) or a channel
// webhook URL (used instead of the bot when it's filled in).
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultDiscordMessage = "✨ Shiny {pokemon} (Lv {level}) found in {game}! Encounters: {encounters}"
	defaultDiscordDelay   = 5 // seconds (of game time) between the Pokémon appearing and the picture
)

// discordTarget: is there somewhere to post to (alerts needn't be switched on: the test button)
func discordTarget(s Settings) bool {
	return strings.TrimSpace(s.DiscordWebhook) != "" ||
		(strings.TrimSpace(s.DiscordToken) != "" && strings.TrimSpace(s.DiscordChannel) != "")
}

// discordActive: alerts are on and there's somewhere to post to
func discordActive(s Settings) bool { return s.DiscordOn && discordTarget(s) }

// discordFrames: how many game frames the script waits after the shiny appears
func discordFrames(s Settings) int {
	d := s.DiscordDelay
	if d <= 0 {
		d = defaultDiscordDelay
	}
	return min(d, 30) * 60
}

// shinyShotDir: where mGBA's script puts the game's picture until it's been posted
func shinyShotDir() string { return filepath.Join(os.TempDir(), "gblink-companion") }

// expandMessage fills in the message's placeholders.
func expandMessage(tmpl string, v map[string]string) string {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = defaultDiscordMessage
	}
	var kv []string
	for k, val := range v {
		kv = append(kv, "{"+k+"}", val)
	}
	return strings.NewReplacer(kv...).Replace(tmpl)
}

// prettySpecies: "MR. MIME" -> "Mr. Mime", "HO-OH" -> "Ho-Oh"
func prettySpecies(s string) string {
	up := true
	return strings.Map(func(r rune) rune {
		out := unicode.ToLower(r)
		if up {
			out = unicode.ToUpper(r)
		}
		up = r == ' ' || r == '-' || r == '.'
		return out
	}, s)
}

type discordFile struct {
	name string
	data []byte
}

var discordClient = &http.Client{Timeout: 30 * time.Second}

// discordSend posts a message (with pictures) to the webhook if there is one, else through the bot.
func discordSend(s Settings, content string, files []discordFile) error {
	if r := []rune(content); len(r) > 2000 { // Discord's limit
		content = string(r[:1997]) + "..."
	}
	payload := map[string]any{"content": content}
	if len(files) > 0 {
		var atts []map[string]any
		for i, f := range files {
			atts = append(atts, map[string]any{"id": i, "filename": f.name})
		}
		payload["attachments"] = atts
	}
	pj, _ := json.Marshal(payload)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="payload_json"`)
	h.Set("Content-Type", "application/json")
	part, _ := mw.CreatePart(h)
	part.Write(pj)
	for i, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files[%d]"; filename="%s"`, i, f.name))
		h.Set("Content-Type", "image/png")
		part, _ := mw.CreatePart(h)
		part.Write(f.data)
	}
	mw.Close()

	target, auth := "", ""
	if hook := strings.TrimSpace(s.DiscordWebhook); hook != "" {
		if !strings.HasPrefix(hook, "https://") {
			return fmt.Errorf("the webhook URL should start with https://")
		}
		target = hook
		if strings.Contains(hook, "?") {
			target += "&wait=true"
		} else {
			target += "?wait=true"
		}
	} else {
		tok := strings.TrimSpace(s.DiscordToken)
		tok = strings.TrimSpace(strings.TrimPrefix(tok, "Bot "))
		ch := strings.TrimSpace(s.DiscordChannel)
		if tok == "" || ch == "" {
			return fmt.Errorf("it needs a bot token and a channel ID (or a webhook URL)")
		}
		target = "https://discord.com/api/v10/channels/" + url.PathEscape(ch) + "/messages"
		auth = "Bot " + tok
	}

	for try := 0; ; try++ {
		req, err := http.NewRequest("POST", target, bytes.NewReader(body.Bytes()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("User-Agent", "DiscordBot (https://github.com/Shinygati/GB-Link-Companion, 1)")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := discordClient.Do(req)
		if err != nil {
			return err
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		switch {
		case resp.StatusCode < 300:
			return nil
		case resp.StatusCode == 429 && try == 0: // rate limited: wait as long as it says (not long), once
			var rl struct {
				RetryAfter float64 `json:"retry_after"`
			}
			json.Unmarshal(msg, &rl)
			time.Sleep(time.Duration(min(max(rl.RetryAfter, 1), 10) * float64(time.Second)))
			continue
		case resp.StatusCode == 401:
			return fmt.Errorf("Discord didn't accept the bot token (401)")
		case resp.StatusCode == 403:
			return fmt.Errorf("the bot isn't allowed to post there (403): it needs View Channel, Send Messages and Attach Files")
		case resp.StatusCode == 404:
			return fmt.Errorf("Discord can't find that channel or webhook (404)")
		}
		text := strings.TrimSpace(string(msg))
		if len(text) > 200 {
			text = text[:200]
		}
		return fmt.Errorf("Discord said %d: %s", resp.StatusCode, text)
	}
}

// zoomPNG reads a picture and returns it as a PNG zoom times bigger (square dots, like the GBA).
func zoomPNG(path string, zoom int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*zoom, b.Dy()*zoom))
	for y := 0; y < b.Dy()*zoom; y++ {
		for x := 0; x < b.Dx()*zoom; x++ {
			dst.Set(x, y, src.At(b.Min.X+x/zoom, b.Min.Y+y/zoom))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ------------------------------------------------------------------------------ the bridge

// shinyJob: a shiny the script reported ("Y" line): its species, level and the game's picture.
type shinyJob struct{ species, level, shot string }

// handleShiny runs on pollLoop's goroutine (it uses the status screens' state): it makes the wild
// info picture for each shiny the script reported and posts them from a goroutine of their own.
func (b *Bridge) handleShiny() {
	b.mu.Lock()
	q := b.shinyQ
	b.shinyQ = nil
	b.mu.Unlock()
	if len(q) == 0 {
		return
	}
	b.countEncounters() // the shiny counts too
	for _, j := range q {
		set := b.s.app.settings()
		if !discordActive(set) {
			if j.shot != "" {
				os.Remove(j.shot)
			}
			continue
		}
		var info []byte
		if set.DiscordShots && j.shot != "" {
			info = b.wildInfoPNG()
		}
		enc := commaInt(b.st.encSession)
		if gs := b.s.state; gs.EncTracking {
			enc = commaInt(gs.EncCount)
		}
		game := titleCase(b.game)
		if b.s.game != nil && b.s.game.Title != "" {
			game = b.s.game.Title
		}
		vars := map[string]string{"pokemon": prettySpecies(j.species), "level": j.level, "game": game, "encounters": enc}
		go b.postShiny(set, j, info, vars)
	}
}

// postShiny sends the message, trying a few times in case the network blinks.
func (b *Bridge) postShiny(set Settings, j shinyJob, info []byte, vars map[string]string) {
	var files []discordFile
	if j.shot != "" {
		if set.DiscordShots {
			if data, err := zoomPNG(j.shot, 3); err == nil {
				files = append(files, discordFile{"shiny-encounter.png", data})
			} else {
				b.s.ui.Log("Couldn't read the shiny's screenshot (%v); posting without it.", err)
			}
			if info != nil {
				files = append(files, discordFile{"shiny-info.png", info})
			}
		}
		os.Remove(j.shot)
	}
	text := expandMessage(set.DiscordMessage, vars)
	var err error
	for try := 0; try < 3; try++ {
		if err = discordSend(set, text, files); err == nil {
			b.s.ui.Log("A shiny %s: posted to Discord.", vars["pokemon"])
			return
		}
		if try < 2 {
			time.Sleep(time.Duration(5*(try+1)) * time.Second)
		}
	}
	b.s.ui.Log("Couldn't post the shiny %s to Discord: %v", vars["pokemon"], err)
}

// wildInfoPNG draws the wild Pokémon's info screen (the one the GBA shows in its Wild menu) from the
// script's latest data, whatever the GBA is showing now. Only pollLoop's goroutine may call it.
func (b *Bridge) wildInfoPNG() []byte {
	st := &b.st
	b.mu.Lock()
	have := b.screens[modeWild] != nil
	b.mu.Unlock()
	if !have {
		return nil
	}
	mode, scroll := st.mode, st.scroll // compose() works on the current mode: borrow it
	st.mode, st.scroll = modeWild, 0
	t, _, want, spriteData := b.compose()
	st.mode, st.scroll = mode, scroll
	t[stRows-1] = blankText()[stRows-1] // not the "hold SELECT" hint

	g := &gbaScreen{}
	g.setJapanese(b.lang == 'J')
	g.apply([]uint32{stOpClear, 0}, nil)
	var rows []uint32
	for r := 0; r < stRows; r++ {
		rows = append(rows, rowWords(r, t[r])...)
	}
	g.apply([]uint32{stOpRows, uint32(len(rows))}, rows)
	for slot := 0; slot < stSprites; slot++ {
		p, ok := want[slot]
		if !ok {
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
		g.apply([]uint32{stOpSprite, uint32(len(words)), uint32(slot), uint32(p.x), uint32(p.y), uint32(p.w), uint32(p.h), flags}, words)
	}
	img := g.image(scShotZoom)
	if img == nil {
		return nil
	}
	var out bytes.Buffer
	if png.Encode(&out, img) != nil {
		return nil
	}
	return out.Bytes()
}

// optionLines: the settings the script is sent as lines (F: Emerald's seed; Q: the roamer's
// location; X: shiny pictures)
func (b *Bridge) optionLines() string {
	set := b.s.app.settings()
	opt := "F1\n" // fix Emerald's starting seed
	if set.KeepEmeraldSeed {
		opt = "F0\n"
	}
	if b.s.game != nil { // where the roamer is, found in the ROM (roamer.go)
		opt += roamerOption(b.s.game.ROM)
	}
	if discordActive(set) {
		shots := "0"
		if set.DiscordShots {
			shots = "1"
		}
		return opt + "X1\t" + strconv.Itoa(discordFrames(set)) + "\t" + shots + "\t" + shinyShotDir() + "\n"
	}
	return opt + "X0\t0\t0\t\n"
}

// ------------------------------------------------------------------------------ the window

// discordTest posts a test message (no pictures) so the settings can be checked.
func (a *App) discordTest(s Settings) {
	if !discordTarget(s) {
		a.ui.Toast("Fill in a bot token and channel ID, or a webhook URL, first.")
		return
	}
	text := expandMessage(s.DiscordMessage, map[string]string{"pokemon": "Pikachu", "level": "5", "game": "Test", "encounters": "1,234"})
	if err := discordSend(s, "(test) "+text, nil); err != nil {
		a.ui.Toast("Discord: " + err.Error())
		return
	}
	a.ui.Toast("Test message sent to Discord.")
}

// discordFields returns what to change in the settings to match what's typed in the window's boxes.
func (w *Window) discordFields() func(*Settings) {
	tok := strings.TrimSpace(w.dcToken.Text())
	ch := strings.TrimSpace(w.dcChannel.Text())
	hook := strings.TrimSpace(w.dcHook.Text())
	msg := strings.TrimSpace(w.dcMsg.Text())
	delay, _ := strconv.Atoi(strings.TrimSpace(w.dcDelay.Text()))
	if delay < 0 {
		delay = 0
	}
	return func(s *Settings) {
		s.DiscordToken, s.DiscordChannel, s.DiscordWebhook, s.DiscordMessage, s.DiscordDelay = tok, ch, hook, msg, delay
	}
}

// loadDiscordFields puts the saved settings into the boxes (when the settings window opens).
func (w *Window) loadDiscordFields(s Settings) {
	w.dcToken.SetText(s.DiscordToken)
	w.dcChannel.SetText(s.DiscordChannel)
	w.dcHook.SetText(s.DiscordWebhook)
	w.dcMsg.SetText(s.DiscordMessage)
	if s.DiscordDelay > 0 {
		w.dcDelay.SetText(strconv.Itoa(s.DiscordDelay))
	} else {
		w.dcDelay.SetText("")
	}
}
