# GB-Link Companion

> **Not affiliated with GB-Link.** GB-Link Companion is an independent, unofficial program. It is not made, endorsed or supported by the makers of the GB-Link adapter ([gblink.io](https://gblink.io)).

Play your GBA games in **mGBA** with the **save from your real cartridge**, using a [GB-Link](https://gblink.io) adapter. Pick a game, press **Play**, and the app does the rest:

1. **Loads your save** from the cartridge into mGBA.
2. **Keeps the cartridge up to date** while you play: every time you save in-game, it's copied to the cartridge.
3. **Saves back to the cartridge** when you close mGBA.

While you play, the **GBA's buttons work as a controller** for mGBA, or, in Pokémon games, the GBA can show **live info from the game**: the RNG, the wild Pokémon (with catch and flee chances), the trainer's team, and the map.

It can also **back up your cartridges**: it copies the game (ROM) and its save to your PC.

> ⚠️ **Status:** tested end to end against a simulated GB-Link, GBA and save chips, but **not yet on real hardware**. The app backs up your cartridge's save every time, but try it with a save you can afford to lose first.

No Python, web browser or anything else needed: it's a single `GBLinkCompanion.exe` with its own window.

---

## What you need

- **Windows 10 or 11.**
- **A GB-Link adapter** with current firmware, and a **Game Boy Color link cable**.
- **A GBA or GBA SP**, with the game's cartridge in it.
- **mGBA.** The app finds it in its usual install folders, or you can point it to `mGBA.exe`.
- **Your game's ROM** (`.gba` file), the same game as the cartridge.

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

## Backing up a cartridge

Press **Back up a cartridge** on the game list.

1. Switch the GBA on while holding START and SELECT, with the cartridge in it.
2. The app reads the cartridge's name, code and size, and shows where it'll save the backup.
   - It saves to your games folder, as for example `POKEMON EMER (BPEE).gba`.
   - Press **Save somewhere else…** to choose another place.
3. Press **Back it up** and keep the GBA on until it's done.
   - The progress bar shows how much is copied and roughly how long is left. Expect about 30 KB a second over the link cable: around 9 minutes for a 16 MB game, 18 minutes for 32 MB.
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
| **Map location** | The game's own region map with your icon, the place's name and your X/Y, and how many trainers you haven't battled and hidden items you haven't found there. **A** shows a cursor (move it with the D-pad to see any place's numbers); **A** again shows a zoomed-out picture of that place; **B** goes back. |
| **Encounter tracker** | Counts every wild Pokémon you meet while it's on (**START**), with the last one seen. **A** sets the count digit by digit. Saved per game. |

On the GBA: **Up/Down** and **A** choose in the menu, **B** goes back to it, **Left/Right** go to the next screen, and **Up/Down** (or **L/R** a page at a time) scroll a long screen.

**Play while you watch a screen:** on any status screen, **hold SELECT** for half a second and the GBA's buttons control mGBA while the screen stays up (the bottom line turns yellow). Hold SELECT again to get the screen's buttons back. A quick SELECT press still reaches the game.

- **Games:** Pokémon Ruby, Sapphire, Emerald, FireRed and LeafGreen, **English**, every revision (Ruby/Sapphire 1.0–1.2, FireRed/LeafGreen 1.0–1.1).
- **Shinygati's QoL builds:** [pokeemeraldQoL](https://github.com/Shinygati/pokeemeraldQoL) (Emerald) and [pokefireredQoL](https://github.com/Shinygati/pokefireredQoL) (FireRed/LeafGreen v1.1), as built from their `master` branches with `make` (agbcc). The menu has no RNG screen for these.
- **Your own builds of the decomps** (including newer QoL builds, whose data moves whenever the code changes): put the build's symbol file next to the ROM: `pokeemerald.sym` (from `make syms`) or `pokeemerald.elf`, in the same folder as the ROM (the ROM can have another name). The app checks it belongs to that ROM (the Log says if it doesn't) and reads the addresses from it; the menu has no RNG screen for these either.
- **Other ROM hacks** move the game's data around, so the screens say they can't read them; controller mode still works.
- **Other games:** the controller works as usual; the screens say the game isn't supported.
- **Pokémon Emerald's starting seed.** The original Emerald never seeds its RNG, so it starts from the same seed every time. When it starts in mGBA, the script gives it a random 16-bit seed instead, the way the game was meant to (the RNG screen marks it as set by GB-Link Companion). Turn **Fix Emerald's starting seed** off in Settings if your RNG manipulation relies on the old seed.
- **The initial seed** is the 16-bit seed the game started from. If the game has been running for a long time before the script started, the app can't be sure which it was, so it counts the advances from when the script started instead.
- **When you save,** the GBA shows the save being copied to the cartridge, then goes back to the screen you were on.
- The screens come from the same mGBA script as the controller (`gblink_controller.lua`), so it needs to be loaded (see below). The app remembers which screen you were on.

---

## Wonder Cards (Emerald, FireRed, LeafGreen)

While you play, **Wonder Cards…** on the play screen lists every card from the GB-Link team's [gblink-wondercards](https://github.com/GB-Link/gblink-wondercards) (GPL-3.0) for your game: the event distributions (Aurora, Mystic and Eon Tickets, Wish Eggs and more), Project Wonder's cards and the GB-Link Team's own. Click a card's name to see what it does, and tick the ones you want.

- **Use now:** the ticked cards' gifts or effects happen straight away, one after another, as if the deliveryman had just handed each one over. Nothing is saved as a Wonder Card, so the card your game already has stays. Cards marked *until reset* (the speed cards, Walk Through Walls, PC Anywhere and others) keep running until the game is reset; they all use the same spare memory, so only one of them can be on at a time. Ticking one unticks the others, and using one while another is on offers **Replace**: the old one is switched off and the new one goes on.
- **Receive Wondercard** (one ticked card): the card is saved the way Mystery Gift saves one. Talk to the deliveryman in green on any Pokémon Center's 2F to get the gift (go in again if you're already there), and save in the game to keep the card. A game keeps one card at a time.
- The ticks are remembered for each game.
- Only the original English games, checked against the No-Intro list of good dumps (a ROM hack or a bad dump can't take them: the cards' code is made for the exact ROMs). Load your game first; a card waits until you can walk around.

## Version and updates

The version is shown beside the name at the top of the window. When the app starts it asks GitHub for the newest release of [Shinygati/GB-Link-Companion](https://github.com/Shinygati/GB-Link-Companion/releases); if it's newer, an **Update to v…** button appears at the top, which opens the release page. **Settings › Check for updates** checks again. To publish an update: change `appVersion` in `version.go`, build, and make a GitHub release whose tag is the same version (e.g. `v1.0.1`) with `GBLinkCompanion.exe` attached.

## Good to know

- **Backups.** Before a cartridge save replaces the save on your PC, a copy goes into `gblink_backups\<game>\` next to the ROM. Each save read from the cartridge is kept there as well; the newest 30 are kept.
- **Wrong cartridge.** If the cartridge isn't the same game as the ROM, nothing is written, and the app tells you which cartridge it found.
- **Closing the window.** If you close the window while a game is running, the app keeps going in the background. Your save is still copied to the cartridge. If it needs you (for example, to switch the GBA on), the window opens again by itself. If everything went fine, it just quits when you close mGBA.
- **If a save couldn't be copied.** It's kept on your PC. The next time you play that game, the app offers to copy it to the cartridge first.
- **Every write is checked.** Each save written to the cartridge is read back and verified.
- **Where settings live.** The app's settings are kept in `%APPDATA%\GBLinkSaveSync`.

---

## Building from source

You need Go 1.24 or newer.

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GBLinkCompanion.exe .
```

The GBA helper (`assets/gblink_sync_mb.gba`) and the mGBA script (`assets/gblink_controller.lua`) are built into the app. To change the helper, build it from `b2s/gba` with devkitARM or `arm-none-eabi-gcc` and `gbafix` on your PATH, then copy it into `assets/` before building the app:

```
make -C ../b2s/gba
cp ../b2s/gba/gblink_sync_mb.gba assets/
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
| `assets/gblink_sync_mb.gba` | The helper program sent to the GBA: [Burn2Slot for GB-Link](../b2s), built with this app's own screens |
| `symfile.go` | Your own builds: reads the `.sym`/`.elf` next to the ROM for the script's addresses |
| `status.go` | The GBA's menu and status screens: reads the GBA's buttons, gets screens from the mGBA script, and sends the GBA what changed |
| `assets/gblink_controller.lua` | The mGBA script: the GBA's buttons into mGBA, and reading the Pokémon game for the status screens. Its addresses come from pret's symbol files ([pokeruby](https://github.com/pret/pokeruby), [pokeemerald](https://github.com/pret/pokeemerald), [pokefirered](https://github.com/pret/pokefirered), `symbols` branches) |
| `test/status_test.py` | End-to-end test of the status screens: the real helper running in mGBA as the GBA (with screenshots of its screen), and the real script running in libmgba with a test cart (`test/status/`). See `test/status/README.md` |
| `winres/` | The exe's icon and version info (`go run github.com/tc-hib/go-winres@v0.3.3 make`) |
| `backup.go` | Backing up a cartridge: the game and its save |
| `assets/gba.png` | The GBA picture shown when you're asked to switch it on |
| `nointro.go`, `libverify.go` | No-Intro checks: the built-in list (`assets/nointro-gba.tsv.gz`), loading DAT files, and hashing the games in the list |
| `test/library_test.py` | The game list's No-Intro check, and loading a DAT file |
| `test/repro_test.py` | Backs up a real ROM (pass its path) from a simulated 32 MB repro cart, with erased flash after it and with an old game after it |
| `test/backup_test.py` | End-to-end test of a cartridge backup: it checks the `.gba` and `.sav` byte for byte, and that Cancel leaves nothing behind |
| `test/native_test.py` | End-to-end test of the Linux build (`go build -tags testhooks`) on a virtual display: a fake GB-Link, a simulated GBA running the real helper code, and a stand-in mGBA. It uses real mouse clicks for Continue and for double-clicking a game, and takes screenshots. Run `native_test.py APP SHOTS [off|close]`. It needs the Burn2Slot simulator (`b2s/sim/sim`), Xvfb, xdotool, ImageMagick and python-xlib. |

---

## Credits

- **Multiboot:** ported from [GB-Link Multiboot Web](https://github.com/GB-Link/gba-multiboot-web) (GPL-3.0).
- **GBA helper:** the Burn2Slot for GB-Link flasher. Its save code follows [GB-Link Cart Doctor](https://github.com/GB-Link/gb-cart-doctor-web), and its ROM flashing comes from [Burn2Slot](https://github.com/vrodin/Burn2Slot).
- **Game database:** [No-Intro](https://no-intro.org)'s Game Boy Advance DAT (built in from the copy in [libretro-database](https://github.com/libretro/libretro-database)).
- **Serial port library:** [go.bug.st/serial](https://github.com/bugst/go-serial) (BSD-3-Clause).
- **UI toolkit:** [Gio](https://gioui.org) (MIT / Unlicense). Icons: Material Design icons (Apache-2.0).
- **Pokémon game data:** addresses and layouts from the [pret](https://github.com/pret) decompilations. Sprites, names and maps are read from your own game in mGBA; none are included.
- **Status screen font:** [Spleen](https://github.com/fcambus/spleen) 5x8 by Frederic Cambus (BSD-2-Clause).

GPL-3.0.
