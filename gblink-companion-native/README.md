# GB-Link Companion

> **Not affiliated with GB-Link.** GB-Link Companion is an independent, unofficial program. It is not made, endorsed or supported by the makers of the GB-Link adapter ([gblink.io](https://gblink.io)).

*Created by Shinygati. Version 1.5.0.*

Play your GBA, Game Boy and Game Boy Color games in **mGBA** with the **save from your real cartridge**, using a [GB-Link](https://gblink.io) adapter. Pick a game, press **Play**, and the app does the rest:

1. **Loads your save** from the cartridge into mGBA.
2. **Keeps the cartridge up to date** while you play: every time you save in-game, it's copied to the cartridge.
3. **Saves back to the cartridge** when you close mGBA.

While you play, the **GBA's buttons work as a controller** for mGBA, or, in Pokémon games, the GBA can show **live info from the game**: the RNG, the wild Pokémon (with catch and flee chances), the trainer's team, and the map.

It also has:

- **Cartridge tools:** back up GBA, Game Boy and Game Boy Color cartridges (the game and its save), write a save back to a cartridge, and write a game to a GBA or Game Boy flash cart.
- **Two Pokémon Banks:** one for Emerald, FireRed and LeafGreen, and a separate one for Red, Blue, Yellow, Gold, Silver and Crystal. Pokémon never move between the two.
- **A Home button** at the top of the window that always takes you back to the games list.

> ⚠️ **Status:** tested end to end against a simulated GB-Link, GBA and save chips, but **not yet on real hardware**. The app backs up your cartridge's save every time, but try it with a save you can afford to lose first.

No Python, web browser or anything else needed: it's a single `GBLinkCompanion.exe` with its own window.

---

## What you need

- **Windows 10 or 11.**
- **A GB-Link adapter** with current firmware, and a **Game Boy Color link cable**.
- **A GBA or GBA SP**, with the game's cartridge in it. For Game Boy and Game Boy Color games, the GBA is switched into Game Boy Color mode and the Game Boy cartridge goes in it.
- **mGBA.** The app finds it in its usual install folders, or you can point it to `mGBA.exe`.
- **Your game's ROM** (`.gba`, `.gb` or `.gbc` file), the same game as the cartridge.

Supported cartridge saves: battery SRAM/FRAM, flash 64 KB and 128 KB, and EEPROM 512 B and 8 KB.

---

## How to use

1. Double-click **GBLinkCompanion.exe**.
2. **The first time**, the app checks three things:
   - the GB-Link is plugged in,
   - where mGBA is,
   - which folder your games are in.
3. **Choose a game** from the list. Recently played games are at the top, and you can search. You can also use **Browse for a game…**, or drag a ROM onto `GBLinkCompanion.exe`.
4. Press **Play**.
5. When asked, **switch the GBA on while holding START and SELECT**. The app reads your save, then opens mGBA.
6. **Play and save as usual.** The window shows whether the cartridge is up to date, whether the GBA is on, and the controller.
7. **Close mGBA** when you're done. Your save is copied to the cartridge, and the window says **All done!**

### Switching the GBA off while you play

- You can switch the GBA off at any time.
- Saves made while it's off are kept on your PC.
- To reconnect, switch the GBA on while holding START and SELECT. The app picks it back up by itself and copies your latest save over.
- If the GBA is still off when you close mGBA, the app asks you to switch it on.
- If you'd rather be asked straight away each time you save, turn on **Ask me straight away** in Settings.

---

### Game Boy and Game Boy Color games

`.gb` and `.gbc` games are in the same list. Press **Play**, then:

1. Switch the GBA on while holding START and SELECT, **with no cartridge in it**. The app sends it a program (from GB-Link Cart Doctor) that turns it into a Game Boy Color.
2. When asked, **put the Game Boy cartridge in**. The app checks it's the same game as the ROM.
3. The save is read from the cartridge and mGBA opens with it. Each save you make is written to the cartridge and read back to check it, and the last one goes back when you close mGBA.

As a Game Boy Color, the GBA can't be a controller or a second screen: play with the PC's keyboard or a pad. If it's switched off, your saves wait on the PC.

---

## The Home button

The **Home** button at the top of the window takes you back to the games list from anywhere: a backup or other cartridge tool (it stops it first), the Pokémon Bank (it asks about unsaved changes), Settings, or the end of a game. If a game is still running, it asks before closing mGBA, and your save still goes to the cartridge.

---

## Cartridge tools

Press **Cartridge tools** on the game list. (These replace the old **Restore a save** and **Back up a cartridge** buttons.)

| Tool | What it does |
|---|---|
| Back up a GBA cartridge | The game and its save, checked against No-Intro (see below). |
| Write a save to a GBA cartridge | Replaces the cartridge's save with a `.sav` file; the cartridge's own save is copied to your PC first. |
| Write a ROM to a GBA flash cart | Writes a `.gba` to a flash or repro cart and reads it back. The cart is found from FlashGBX's list of cart types, its CFI table, or the common chip commands. |
| Back up a Game Boy cartridge | A Game Boy or Game Boy Color game and its save, checked against the header's checksums. |
| Write a save to a Game Boy cartridge | Replaces the save with a `.sav` file and reads it back; the old save is copied first. |
| Write a ROM to a Game Boy flash cart | Writes a `.gb`/`.gbc` to a Game Boy flash cart from FlashGBX's list and checks it. |

GBA cartridges go through the GB-Link Companion helper, which now moves saves and games in GB-Link Cart Doctor's fast streamed way. Game Boy cartridges use GB-Link Cart Doctor's own programs, and flash carts its flash code.

### Backing up a GBA cartridge

Press **Cartridge tools › Back up a GBA cartridge**.

1. Switch the GBA on while holding START and SELECT, with the cartridge in it.
2. The app reads the cartridge's name, code and size, and shows where it'll save the backup.
   - It saves to your games folder, as for example `POKEMON EMER (BPEE).gba`.
   - Press **Save somewhere else…** to choose another place.
3. Press **Back it up** and keep the GBA on until it's done.
   - The progress bar shows how much is copied and roughly how long is left. The game is streamed the way GB-Link Cart Doctor does it, and empty stretches of the cartridge are skipped over quickly.
   - **Cancel** stops it and deletes the partial file.

The game's save is copied too, next to the game with the same name, so you can press **Play it now** straight away. The backed-up game is added to your list.

Good to know:
- **How the size is found.** It's read from the cartridge itself:
  - **Official carts:** past the end of the game, the cartridge either answers with a pattern instead of data (the method GB-Link Cart Doctor uses), or its ROM chip repeats from the start (e.g. 16 MB Pokémon carts).
  - **Repro carts** (e.g. "Chis" carts with a 32 MB flash chip): if No-Intro lists the game's code (e.g. BPEE = 16 MB), that size is used. Otherwise the app measures where the game's data ends.
  - **Repro carts with an older game still stored after this one, and a game code No-Intro doesn't list:** the size is a best guess. The app says so and offers **Back up 16 MB** (or whatever size it found) or **Back up all 32 MB**.
- **Checked against No-Intro.** Every backup's SHA-1 is checked against [No-Intro](https://datomatic.no-intro.org/index.php?page=search&s=64)'s list of known good GBA dumps, and the result is shown when it's done:
  - **Verified:** it matches a known good dump, and its No-Intro name is shown.
  - **Can't verify checksum:** No-Intro knows the game, but this copy doesn't match. Re-seat the cartridge and back it up again. Repro carts often hold modified games (for example, patched to save without a battery), so a mismatch is common there.
  - **Not in No-Intro:** a hack, homebrew, or a game No-Intro doesn't list.

  If you chose **Back up all 32 MB** and the first part is a known dump, the backup is trimmed to it. The SHA-1 and CRC32 are shown with **Copy SHA-1** and **Search No-Intro…** buttons.
- **Header check.** The backup is checked against the game header's checksum. If it doesn't match, you're told. That's normal for some repro carts and ROM hacks; on an official cart, re-seat it and back it up again.
- **Your own cartridges only.** Only back up games you own.

---

## Pokémon Bank

**Pokémon Bank** at the top of the window asks which bank to open. The two banks are kept apart: Pokémon move between the games of one bank, never from a Game Boy game to a GBA game or back.

In both banks:
- **Load from cartridge** reads the cartridge's boxes over GB-Link; **Open a save file…** opens a `.sav` (from a flash cart's SD card or an emulator) instead.
- **Move** and **Move Multi** work only by **dragging**: drag a Pokémon onto a slot (one already there swaps places), or choose several in Move Multi and drag one of them to move them all. Clicking a Pokémon shows its summary.
- **Release** is the only action done by clicking: mark Pokémon, then press Release.
- Nothing is written until **Save**. The cartridge (or file) is backed up, written and read back to check it, and only then is the bank on your PC changed. If it stops part way, the next time works out where every Pokémon is.

**GBA bank (Emerald, FireRed, LeafGreen):** `.pk3` files, one folder per box, in the bank's folder (Settings › Pokémon Enhancements).

**Game Boy bank (Red, Blue, Yellow, Gold, Silver, Crystal):** `.pk1` and `.pk2` files in `Pokemon Bank GB`. The saves are read and written with code ported from **PKHeX**, so the box lists, checksums and the second copy of the save are kept right. Between Gold/Silver/Crystal and Red/Blue/Yellow it follows the Time Capsule's rules: only the first 151, no eggs, only generation 1 moves. A **flash cart showing its own menu**, like the **EZ-Flash Junior**, has its save read from its SRAM, where it keeps the last game's save. Every language works, each read with PKHeX's layout for it: the international games, the Japanese ones (boxes of 30) and Korean Gold and Silver. As in the games, Japanese, Korean and international Pokémon stay with games of their own kind (slots show **JP** or **KO**).

**Both banks** work with every language: the GBA bank reads Japanese Emerald, FireRed and LeafGreen too (a Japanese save file is recognised by itself). Japanese and Korean names use your system's Japanese and Korean fonts (Yu Gothic/Meiryo and Malgun Gothic on Windows, Noto Sans CJK on Linux).

---

## Checking your games

Every game in the list is checked against No-Intro's list in the background. Each file is read once, and the result is remembered.
- **Verified:** a known good dump. The No-Intro name is shown under **Selected**.
- **Can't verify checksum:** No-Intro knows the game code, but this file doesn't match (modified, a ROM hack, a patched repro dump, or a bad dump).
- **Not in No-Intro:** the game isn't in the list.

A copy of No-Intro's GBA list is built in. To use a newer one:
1. On [Datomatic](https://datomatic.no-intro.org), download the Game Boy Advance DAT.
2. In **Settings › No-Intro database**, press **Load a DAT file…** and choose it (the `.zip` works too).

---

## The GBA as a controller

The GBA's buttons control the game in mGBA while it's connected. Your keyboard and gamepad keep working too.

mGBA receives the buttons through a small script, `gblink_controller.lua`, which the app writes next to itself.

- **mGBA development builds** (and 0.11 when it's released) load the script automatically.
- **mGBA 0.10.x** can't load scripts when it starts. The app shows the steps; you need to do this each time you start mGBA:
  1. **Tools › Scripting…**
  2. **File › Load script…**
  3. Choose `gblink_controller.lua`.

  The **Copy its location** button puts the file's path on the clipboard, so you can paste it into the file box.

You can turn the controller off in Settings.

---

## Status screens on the GBA (Pokémon games)

While you play, the GBA has a menu (it opens on it the first time). When mGBA closes, the GBA goes back to its own screen.

| Menu item | What the GBA shows |
|---|---|
| **Use GBA as controller** | The GBA's buttons control mGBA, as before. Hold **SELECT** for half a second to get back to the menu (a quick SELECT press still reaches the game). |
| **RNG state** | The current seed, the initial seed, how many advances since then, and the next shiny frame for your TID/SID with its PID, nature and IVs. |
| **Wild Pokémon** | The wild Pokémon in the battle, with its front sprite (shiny colours if it's shiny): level, gender, nature, ability, IVs, HP, status, PID and the frame it was made on. Also its catch rate, the catch rate right now after its HP and status, and the chance of catching it with a Poké, Great and Ultra Ball this turn. |
| ↳ in the **Safari Zone** | The same, then the Safari Ball's chance, the flee chance each turn, and the rocks/bait (FireRed, LeafGreen) or Pokéblocks/going near (Ruby, Sapphire, Emerald) that changed them. |
| **Trainer's team** | The trainer's whole team with party icons, including Pokémon not sent out yet: level, gender, nature, ability, HP, PID and IVs, and which one is out or has fainted. |
| **Map location** | The game's own region map with your icon, the place's name and your X/Y, and how many trainers you haven't battled and hidden items you haven't found there. In FireRed and LeafGreen it switches to the Sevii Islands' maps (One–Three, Four–Five, Six–Seven) when you're there, as the game's own map does. A **roaming Pokémon** (Latias/Latios, or Entei/Raikou/Suicune) shows as its icon where it is now; put the cursor on it to see which one and its level. **A** shows a cursor (move it with the D-pad to see any place's numbers); **A** again shows a zoomed-out picture of that place; **B** goes back. |
| **Encounter tracker** | Counts every wild Pokémon you meet while it's on (**START**), with the last one seen. **A** sets the count digit by digit. Saved per game. |

On the GBA: **Up/Down** and **A** choose in the menu, **B** goes back to it, **Left/Right** go to the next screen, and **Up/Down** (or **L/R** a page at a time) scroll a long screen.

**Play while you watch a screen:** on any status screen, **hold SELECT** for half a second and the GBA's buttons control mGBA while the screen stays up (the bottom line turns yellow). Hold SELECT again to get the screen's buttons back. A quick SELECT press still reaches the game.

- **Games:** Pokémon Ruby, Sapphire, Emerald, FireRed and LeafGreen, every revision, in English, French, German, Italian, Spanish and Japanese. The GBA's helper, menu and screens are in the game's language (Japanese in kana). For the games other than English and German Ruby/Sapphire, the app finds where the game keeps its data when you press Play, and the Log says so (or why it couldn't).
- **Shinygati's QoL builds:** [pokeemeraldQoL](https://github.com/Shinygati/pokeemeraldQoL) (Emerald) and [pokefireredQoL](https://github.com/Shinygati/pokefireredQoL) (FireRed/LeafGreen v1.1), as built from their `master` branches with `make` (agbcc). The menu has no RNG screen for these.
- **Your own builds of the decomps** (including newer QoL builds, whose data moves whenever the code changes): put the build's symbol file next to the ROM: `pokeemerald.sym` (from `make syms`) or `pokeemerald.elf`, in the same folder as the ROM (the ROM can have another name). The app checks it belongs to that ROM (the Log says if it doesn't) and reads the addresses from it; the menu has no RNG screen for these either.
- **ROM hacks with a .sym file:** a hack built from pret's pokeruby, pokeemerald or pokefirered that comes with its symbol file works the same way: put the `.sym` (or `.elf`) next to the ROM. Hacks that renamed Pokémon or natures, or gave the ROM a game code of its own, are fine: the app checks the names are where the symbols say, and tells which game the hack is built from by its symbols. Hacks built on **pokeemerald-expansion** lay their Pokémon data out differently and aren't supported yet (the Log says so).
- **Other ROM hacks** (no .sym) move the game's data around, so the screens say they can't read them; controller mode still works.
- **Other games:** the controller works as usual; the screens say the game isn't supported.
- **Pokémon Emerald's starting seed.** The original Emerald never seeds its RNG, so it starts from the same seed every time. When it starts in mGBA, the script gives it a random 16-bit seed instead, the way the game was meant to (the RNG screen marks it as set by GB-Link Companion). Turn **Fix Emerald's starting seed** off in Settings › Pokémon Enhancements if your RNG manipulation relies on the old seed.
- **The initial seed** is the 16-bit seed the game started from. If the game has been running for a long time before the script started, the app can't be sure which it was, so it counts the advances from when the script started instead.
- **When you save,** the GBA shows the save being copied to the cartridge, then goes back to the screen you were on.
- The screens come from the same mGBA script as the controller (`gblink_controller.lua`), so it needs to be loaded (see below). The app remembers which screen you were on.

---

## Shiny alerts on Discord

Settings › Pokémon Enhancements › **Discord shiny alerts** posts to a Discord channel when a shiny wild Pokémon appears in mGBA.

- **Bot:** make an application at discord.com/developers, add a bot, invite it to your server with *View Channel*, *Send Messages* and *Attach Files*, then paste its token and the channel's ID (Discord's Developer Mode lets you copy it). **Or** paste a channel **webhook URL**, which is used instead of the bot when it's filled in.
- **Message:** your own text, with `{pokemon}`, `{level}`, `{game}` and `{encounters}` filled in (the encounter counter, or this session's count when the counter is off). `@here` and `<@user>` work as in any Discord message.
- **Pictures:** the encounter as mGBA shows it, and the wild Pokémon's info screen (the GBA's *Wild* screen, whatever the GBA is showing). The script waits a few seconds of game time after the Pokémon appears (5 by default; the setting is in the same place) so it's fully on screen.
- **Send a test message** checks the token and channel without waiting for a shiny.

The token is kept in the app's settings file on your PC, so only use a bot made for this.

## Wonder Cards (Emerald, FireRed, LeafGreen)

While you play, **Wonder Cards…** on the play screen lists every card from the GB-Link team's [gblink-wondercards](https://github.com/GB-Link/gblink-wondercards) (GPL-3.0) for your game: the event distributions (Aurora, Mystic and Eon Tickets, Wish Eggs and more), Project Wonder's cards and the GB-Link Team's own. Click a card's name to see what it does, and tick the ones you want.

- **Use now:** the ticked cards' gifts or effects happen straight away, one after another, as if the deliveryman had just handed each one over. Nothing is saved as a Wonder Card, so the card your game already has stays. Cards marked *until reset* (the speed cards, Walk Through Walls, PC Anywhere and others) keep running until the game is reset; they all use the same spare memory, so only one of them can be on at a time. Ticking one unticks the others, and using one while another is on offers **Replace**: the old one is switched off and the new one goes on.
- **Receive Wondercard** (one ticked card): the card is saved the way Mystery Gift saves one. Talk to the deliveryman in green on any Pokémon Center's 2F to get the gift (go in again if you're already there), and save in the game to keep the card. A game keeps one card at a time.
- The ticks are remembered for each game.
- **Your own cards:** Settings › Pokémon Enhancements › **Add .wc3 files…** (or **Add your own .wc3…** in the list) copies `.wc3` files into the app's settings folder, and they show under **Your cards** for every game of their kind (the English/European games' cards for those, the Japanese games' for the Japanese ones). The list shows what each card says. A card's script is made for one game and language, so only use ones made for the game you're playing. **Remove** takes one out again.
- **ROM hacks:** a hack of Emerald, FireRed or LeafGreen that comes with its `.sym` (next to the ROM, as for the status screens) can take **your own** `.wc3` cards, ones made for that hack; the GB-Link team's cards only go to the original games. Receiving a card writes it into the save, and a hack may have moved things in it, so the app reads where the hack keeps the card, its script and the event flags from the hack's own code (at the addresses in the `.sym`, each checked by a second routine). If it can't be sure, cards aren't offered and the play screen says why. The list shows a warning to read (and **I understand**) before anything can be sent: a card made for another game can freeze the game or damage the save, so back up your save first.
- **Wonder Cards while you play** (Settings › Pokémon Enhancements) hides the Wonder Cards card on the play screen if you don't want it. It only shows for Emerald, FireRed and LeafGreen anyway.
- Every language's Emerald, FireRed and LeafGreen, each with the cards made for it (the Japanese games: Use now only). Only the original games, checked against the No-Intro list of good dumps (a ROM hack or a bad dump can't take them: the cards' code is made for the exact ROMs). Load your game first; a card waits until you can walk around.

## Wonder Cards on your GBA (retail English Emerald)

**Wonder Cards on your GBA** (Settings › Pokémon Enhancements › **Start…**) sends cards to Pokémon Emerald running on your real GBA, through the GB-Link, without mGBA:

1. Press it, then with Emerald in the GBA hold **START and SELECT** while you switch it on. The app sends a small program; the screen flashes yellow, then green, and Emerald starts as usual. Your save isn't touched.
2. Continue your game and walk around. The app shows **Ready**.
3. **Wonder Cards…** works as it does in mGBA: **Use now** runs a card right where you stand, **Receive Wondercard** saves it for the deliveryman. Save in the game to keep what the cards give you.
4. Press **Done** when you're finished; Emerald keeps running.

The link cable stays in while you send cards. They go while you're walking around (not in a battle, a menu or the Cable Club: there the link port is the game's). Only the retail English cartridge (USA/Europe, the one revision there is): a red screen means the cartridge is another game, another language or a modified one.

How it works: the program checks the cartridge, puts a small helper into spare memory and starts Emerald from just past the point where it clears that memory, with the helper chained onto the game's interrupts, so it runs alongside the game. That way into the game comes from [gen3-poke-multiplayer](https://github.com/Wired-Ariel/gen3-poke-multiplayer) by Wired-Ariel. The helper does what the mGBA script does with a card. Source: `b2s/emerald_wc` (`make`, then copy `emerald_wc_mb.gba` into `assets/`); `b2s/test/mgba_wc.c` runs it in mGBA behind a pretend GB-Link.

## Version and updates

The version is shown beside the name at the top of the window. When the app starts it asks GitHub for the newest release of [Shinygati/GB-Link-Companion](https://github.com/Shinygati/GB-Link-Companion/releases); if it's newer, an **Update to v…** button appears at the top, which opens the release page. **Settings › Check for updates** checks again. To publish an update: change `appVersion` in `version.go`, build, and make a GitHub release whose tag is the same version (e.g. `v1.0.1`) with `GBLinkCompanion.exe` attached.

## Good to know

- **Backups.** Before a cartridge save replaces the save on your PC, a copy goes into `gblink_backups\<game>\` next to the ROM. Each save read from the cartridge is kept there as well; the newest 30 are kept.
- **Wrong cartridge.** If the cartridge isn't the same game as the ROM, nothing is written, and the app tells you which cartridge it found.
- **Closing the window.** If you close the window while a game is running, the app keeps going in the background. Your save is still copied to the cartridge. If it needs you (for example, to switch the GBA on), the window opens again by itself. If everything went fine, it just quits when you close mGBA.
- **If a save couldn't be copied.** It's kept on your PC. The next time you play that game, the app offers to copy it to the cartridge first.
- **Every write is checked.** Each save written to the cartridge is read back and verified.
- **Where settings live.** The app's settings are kept in `%APPDATA%\GBLinkCompanion`.

---

## On Linux

Run `GBLinkCompanion-1.5.0-x86_64.AppImage` (make it executable first: `chmod +x`, or tick "Allow
executing" in its properties). It works on any desktop Linux from about 2018 on (glibc 2.28 or newer).

- **mGBA** is found by itself if it's installed from your distribution (`mgba-qt`), as an AppImage
  (`mGBA-*.appimage` in ~/Applications, ~/Downloads, ~/Desktop, your home folder, or next to this
  app), or as the Flatpak from Flathub (`io.mgba.mGBA`; the app gives it access to your game's folder).
  Otherwise press "Find mGBA…" in Settings.
- **mGBA 0.10** (what most distributions ship) can't load the GBA-controller script by itself:
  the app then shows how to load it from Tools › Scripting. mGBA 0.11 / development builds load it
  automatically.
- **The GB-Link adapter**: if Linux won't let the app open it, add yourself to the group that owns
  serial ports and log in again: `sudo usermod -aG dialout $USER` (on Arch and some others: `uucp`).
- **File pickers** use zenity (GNOME and most desktops) or kdialog (KDE).
- The app follows your desktop's dark style.

## Building from source

You need Go 1.24 or newer.

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GBLinkCompanion.exe .
```

The Linux AppImage: `packaging/linux/build-appimage.sh` (what it needs is listed at its top).

The GBA helper (`assets/gblink_companion_mb.gba`) and the mGBA script (`assets/gblink_controller.lua`) are built into the app. To change the helper, build it from `b2s/gba` with devkitARM or `arm-none-eabi-gcc` (and `gbafix`, or Python 3 for `../b2s/tools/gbafix.py`), then copy it into `assets/` before building the app:

```
make -C ../b2s/gba
cp ../b2s/gba/gblink_companion_mb.gba assets/
```

| File | What it is |
|---|---|
| `main.go` | Start-up, and making sure only one copy runs (a second launch brings the window forward) |
| `gui.go` | The window, drawn with [Gio](https://gioui.org) (Direct3D 11 on Windows; no web browser) |
| `state.go` | What the window shows, prompts and their answers, the game list |
| `session.go` | Loading the save, watching mGBA's `.sav`, writing to the cartridge, and the GBA controller |
| `link.go` | GB-Link serial transport, GBA multiboot, and the helper program's protocol |
| `files.go` | Settings, per-game state, saves and backups |
| `platform_windows.go` | Windows file and folder pickers |
| `assets/gblink_companion_mb.gba` | The helper program sent to the GBA: [Burn2Slot for GB-Link](../b2s), built with this app's own screens |
| `symfile.go` | Your own builds: reads the `.sym`/`.elf` next to the ROM for the script's addresses |
| `roamer.go` | Finds where the roaming Pokémon is kept (`sRoamerLocation`) in any ROM, for the map screen |
| `wondercards.go`, `wcgui.go`, `wccustom.go`, `wcsym.go` | Wonder Cards: the GB-Link team's cards, the list in the window, your own `.wc3` files, and ROM hacks with a `.sym` |
| `status.go` | The GBA's menu and status screens: reads the GBA's buttons, gets screens from the mGBA script, and sends the GBA what changed |
| `assets/gblink_controller.lua` | The mGBA script: the GBA's buttons into mGBA, and reading the Pokémon game for the status screens. Its addresses come from pret's symbol files ([pokeruby](https://github.com/pret/pokeruby), [pokeemerald](https://github.com/pret/pokeemerald), [pokefirered](https://github.com/pret/pokefirered), `symbols` branches) |
| `test/status_test.py` | End-to-end test of the status screens: the real helper running in mGBA as the GBA (with screenshots of its screen), and the real script running in libmgba with a test cart (`test/status/`). See `test/status/README.md` |
| `winres/` | The exe's icon and version info (`go run github.com/tc-hib/go-winres@v0.3.3 make`) |
| `backup.go` | Backing up a GBA cartridge: the game and its save |
| `stream.go` | The helper's fast streamed save and ROM transfers (GB-Link Cart Doctor's way) |
| `carttools.go`, `toolsgui.go` | The Cartridge tools page and its tools; the Pokémon Bank's choice of bank |
| `gbcart.go`, `gbsession.go` | Game Boy cartridges through GB-Link Cart Doctor's Game Boy Color mode and cartridge programs (`assets/cartdoctor/`); playing Game Boy games |
| `flashprofiles.go`, `gbflash.go`, `gbaflash.go`, `gbacart.go` | Flash cart writing: FlashGBX's cart types (`assets/flashcarts/`), CFI, and GB-Link Cart Doctor's GBA cartridge program |
| `bank*.go` | The GBA Pokémon Bank |
| `gen12.go`, `gen12_text.go`, `gbbank.go`, `gbbankgui.go` | The Game Boy Pokémon Bank: Red/Blue/Yellow and Gold/Silver/Crystal saves (international, Japanese and Korean) and Pokémon, ported from PKHeX (`assets/gen12/`; the character tables in `gen12_text.go`) |
| `test/bank_test.py`, `test/gbbank_test.py` | End-to-end tests of the two banks (drag and drop, saving to a cartridge or save file) |
| `assets/gba.png` | The GBA picture shown when you're asked to switch it on |
| `nointro.go`, `libverify.go` | No-Intro checks: the built-in list (`assets/nointro-gba.tsv.gz`), loading DAT files, and hashing the games in the list |
| `test/library_test.py` | The game list's No-Intro check, and loading a DAT file |
| `test/repro_test.py` | Backs up a real ROM (pass its path) from a simulated 32 MB repro cart, with erased flash after it and with an old game after it |
| `test/backup_test.py` | End-to-end test of a cartridge backup: it checks the `.gba` and `.sav` byte for byte, and that Cancel leaves nothing behind |
| `test/native_test.py` | End-to-end test of the Linux build (`go build -tags testhooks`) on a virtual display: a fake GB-Link, a simulated GBA running the real helper code, and a stand-in mGBA. It uses real mouse clicks for Continue and for double-clicking a game, and takes screenshots. Run `native_test.py APP SHOTS [off|close]`. It needs the Burn2Slot simulator (`b2s/sim/sim`), Xvfb, xdotool, ImageMagick and python-xlib. |

---

## Credits

**GB-Link Companion was created by Shinygati.**

- **GB-Link Cart Doctor** by the GB-Link team ([gb-cart-doctor-web](https://github.com/GB-Link/gb-cart-doctor-web), GPL-3.0): the Game Boy Color mode switch, the Game Boy cartridge program, the GBA cartridge dumper, the fast transfers and the flash cart code.
- **PKHeX** by kwsch and contributors ([PKHeX](https://github.com/kwsch/PKHeX), GPL-3.0): the Game Boy Pokémon Bank's save handling, conversions and data.
- **FlashGBX** by Lesserkuma ([FlashGBX](https://github.com/lesserkuma/FlashGBX), GPL-3.0): the flash cart types.
- **Wonder Cards on your GBA:** the way into a running Emerald (start-up hand-off and interrupt hook) from [gen3-poke-multiplayer](https://github.com/Wired-Ariel/gen3-poke-multiplayer) by Wired-Ariel (GPL-3.0).
- **Multiboot:** ported from [GB-Link Multiboot Web](https://github.com/GB-Link/gba-multiboot-web) (GPL-3.0).
- **GBA helper:** the Burn2Slot for GB-Link flasher. Its save code follows [GB-Link Cart Doctor](https://github.com/GB-Link/gb-cart-doctor-web), and its ROM flashing comes from [Burn2Slot](https://github.com/vrodin/Burn2Slot).
- **Game database:** [No-Intro](https://no-intro.org)'s Game Boy Advance DAT (built in from the copy in [libretro-database](https://github.com/libretro/libretro-database)).
- **Serial port library:** [go.bug.st/serial](https://github.com/bugst/go-serial) (BSD-3-Clause).
- **UI toolkit:** [Gio](https://gioui.org) (MIT / Unlicense). Icons: Material Design icons (Apache-2.0).
- **Pokémon game data:** addresses and layouts from the [pret](https://github.com/pret) decompilations. Sprites, names and maps are read from your own game in mGBA; none are included.
- **Status screen font:** [Spleen](https://github.com/fcambus/spleen) 5x8 by Frederic Cambus (BSD-2-Clause).

GPL-3.0.
