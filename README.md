# GB-Link Companion

An unofficial companion app for the [GB-Link](https://gblink.io) USB adapter, **created by Shinygati**. You play your Game Boy Advance, Game Boy and Game Boy Color games in **mGBA** on your PC with the **save from your real cartridge**. With a GBA game your **GBA** works as the controller, and in the Pokémon games (Ruby, Sapphire, Emerald, FireRed and LeafGreen) it can also be a second screen showing live game info. It also backs up cartridges, writes saves back to them, writes games to flash carts, and has a Pokémon Bank for the GBA games and a separate one for the Game Boy games.

**Current version: 1.5.0.**

> **Not affiliated with GB-Link.** GB-Link Companion is an independent, unofficial program. It isn't made, endorsed or supported by the makers of the GB-Link adapter (gblink.io).

---

## What's new in 1.5.0

- **Home button** at the top of the window: from any screen (a backup, the bank, a finished game, Settings) it goes back to the games list. If a game is running it asks first, and the save still goes to the cartridge.
- **Game Boy and Game Boy Color games**: `.gb` and `.gbc` ROMs show in your games list and play in mGBA with the save on the real cartridge, like the GBA games. See [Game Boy and Game Boy Color games](#game-boy-and-game-boy-color-games).
- **Cartridge tools**: one page for backing up GBA, Game Boy and Game Boy Color cartridges (game and save), writing a save back to a cartridge, and writing a game to a GBA or Game Boy flash cart. It replaces the old *Restore a save* and *Back up a cartridge* buttons. Built on GB-Link Cart Doctor's code.
- **Faster GBA saves and backups**: the GBA helper now streams saves and ROMs the way GB-Link Cart Doctor does, so reading and writing a save and backing up a game take a fraction of the time they used to.
- **Game Boy Pokémon Bank**: a second bank for Pokémon Red, Blue, Yellow, Gold, Silver and Crystal, kept apart from the GBA bank. Save files are read and written with code ported from PKHeX.
- **Open a save file** in both banks: a `.sav` from a flash cart's SD card (such as an EZ-Flash) or an emulator, instead of a cartridge over GB-Link.
- **Flash carts in the banks**: a flash cart showing its own menu (like the EZ-Flash Junior) has its save read straight from its SRAM.
- **Every language** in both banks and when playing: English, French, German, Italian, Spanish and Japanese games (and Korean Gold and Silver in the Game Boy bank), from a cartridge or a save file. Japanese and Korean names are shown with your system's fonts.
- **Drag and drop only** in both banks: every move is a drag (one Pokémon onto another swaps them). Only Release works by clicking.

---

## What it does

### Play with your GBA cartridge save file
Pick a game and press **Play**:

1. The app sends a small helper program to the GBA through the link cable (multiboot). Switch on the GBA while holding START + SELECT.
2. It **reads the save file from the cartridge** via GB-Link and opens the game in mGBA with it.
3. **Every time you save in the game**, the new save is copied back to the cartridge, so the cartridge stays up to date.
4. When you close mGBA, the final save goes back to the cartridge.

Before a save from the cartridge replaces the one on your PC, the app keeps a backup copy. Supported save types are battery SRAM/FRAM, flash 64 KB and 128 KB, and EEPROM 512 B and 8 KB.

### Game Boy and Game Boy Color games
Put `.gb` and `.gbc` ROMs in your games folder and they show in the list with the GBA games. Press **Play**:

1. The app sends a small program to the GBA that switches it into Game Boy Color mode (switch the GBA on while holding START + SELECT, with **no cartridge** in it).
2. When it asks, put the Game Boy cartridge in. The app checks the cartridge is the same game as the ROM (by its title and checksums).
3. The save is read from the cartridge and the game opens in mGBA with it.
4. Every time you save in the game, the save is written to the cartridge and read back to check it. When you close mGBA, the final save goes back too.

Every language's games work: the save is copied byte for byte, whatever the game. In Game Boy Color mode the GBA can't be a controller or a second screen, so play with your keyboard or a pad on the PC. If the GBA is switched off while you play, your save waits on the PC and goes to the cartridge next time. MBC1, MBC2, MBC3 (with its clock), MBC5 and the other common cartridge types are supported, with saves up to 128 KB.

### The GBA as a controller
While you play, the GBA's buttons control the game in mGBA. Hold **SELECT** on the GBA for its menu.

### The GBA as a second screen (Pokémon games only)
This only works with Pokémon Ruby, Sapphire, Emerald, FireRed and LeafGreen. In other games the GBA is a controller only. From the GBA's menu, the GBA shows live information from the Pokémon game running in mGBA:

| Screen | What it shows |
|---|---|
| **RNG state** | The current and initial seed, how many advances, and the next shiny frame for your Trainer ID with its PID, nature and IVs |
| **Wild Pokémon** | Its sprite, nature, ability, IVs, HP, PID, the frame it was made on, catch rate and catch chance per ball. Safari Pokémon also show the flee chance |
| **Trainer** | The opposing trainer's whole team, including Pokémon not sent out yet |
| **Map** | The game's own region map with you on it, remaining trainers and hidden items, and a zoomed-out picture of each area |
| **Encounter tracker** | Counts your wild encounters, saved for each game |

Hold **SELECT** on any status screen to keep playing while it stays up. The screens work with every English revision of the five games, and with ROM hacks built from the pret decompilations (see [ROM hacks on the second screen](#rom-hacks-on-the-second-screen)).

**Emerald's starting seed:** the original Pokémon Emerald always starts its RNG from the same seed. The app gives it a random one, the way the game was meant to. You can turn this off in Settings.

### ROM hacks on the second screen
A ROM hack moves the game's data around, so the second screen has to be told where things are. It reads that from the **symbol file** of the hack's build. This works for hacks built from pret's [pokeemerald](https://github.com/pret/pokeemerald), [pokefirered](https://github.com/pret/pokefirered) and [pokeruby](https://github.com/pret/pokeruby) decompilations, and their forks. It doesn't work for binary-patched hacks (IPS/UPS) that have no build to take the symbols from.

1. **Build the hack** as usual (`make`, or `make firered` and so on).
2. **Make its symbol file:** run `make syms` in the same folder. This writes `pokeemerald.sym` (or `pokefirered.sym`, etc.) next to the `.gba`. The `.elf` file the build makes works too.
3. **Put the `.sym` (or `.elf`) in the same folder as the ROM** that GB-Link Companion plays. It doesn't need the ROM's name; if the folder has several, the app uses the one that matches the ROM.
4. **Press Play.** The app's Log says *"Status screens: using the addresses in pokeemerald.sym"*, and the GBA's screens show the hack. On mGBA 0.10, load the script again after pressing Play.

- **Make the `.sym` again every time you rebuild the hack.** Any change to the code or data moves the addresses, so an old `.sym` won't fit. The app checks that the `.sym` belongs to the ROM. If it doesn't, the Log says *"…isn't from the same build…"*, and the GBA says the game looks like a modified ROM.
- The hack has to keep the original game's code in its header (`BPEE` for Emerald, `BPRE`/`BPGE` for FireRed/LeafGreen, `AXVE`/`AXPE` for Ruby/Sapphire). This is the decomps' default.
- It needs the original games' data layouts, such as how a Pokémon is stored and the save blocks. Hacks that change these a lot aren't supported, for example ones with more than the original 386 species or a reworked Pokémon structure (like pokeemerald-expansion).
- With a hack, the RNG screen is hidden (the RNG may work differently), and Wonder Cards aren't offered (they're made for the original ROMs). The Wild Pokémon, Trainer, Map and Encounter tracker screens work.

<img width="500" height="666" alt="image" src="https://github.com/user-attachments/assets/a956b784-de69-47d7-a644-038d1a4714a1" /> <img width="500" height="666" alt="image" src="https://github.com/user-attachments/assets/7077186e-aac4-4f5d-b936-84b9e4696ab8" />
<img width="500" height="666" alt="image" src="https://github.com/user-attachments/assets/71e85133-4fa4-4f23-b3cb-2feb82e99cca" /> <img width="500" height="666" alt="image" src="https://github.com/user-attachments/assets/ad630bc8-3deb-45d4-8627-4deffa235c1a" />
<img width="500" height="666" alt="image" src="https://github.com/user-attachments/assets/fafefa10-c3ea-4181-a1d4-8940f4db084f" /> <img width="500" height="666" alt="Untitled" src="https://github.com/user-attachments/assets/76fbd8e9-0400-4284-bb60-e6a5b2bfd558" />

### Wonder Cards (Emerald, FireRed, LeafGreen)
While you play, **Wonder Cards…** lists every card from the GB-Link team's [gblink-wondercards](https://github.com/GB-Link/gblink-wondercards) for your game. That includes the event distributions (Aurora, Mystic and Eon Tickets, Wish Eggs and more), Project Wonder's cards and the GB-Link Team's own cards.

- **Use now:** the gifts or effects of the ticked cards happen straight away, one after another, without replacing the card your game already has.
- **Receive Wondercard:** the card is saved the way Mystery Gift saves one, for the deliveryman on any Pokémon Center's 2F.
- Cards marked *until reset* keep running until the game is reset. Only one of them can run at a time, and **Replace** switches from one to another.
- Your ticked cards are remembered for each game.

Wonder Cards only work with the original English games, checked against No-Intro's list of good dumps. Custom ROMs and ROM hacks still work with everything else: the save on your cartridge, the controller, and the second screen.

### Pokémon Bank (Emerald, FireRed, LeafGreen)
**Pokémon Bank** (at the top of the games screen, or Settings › Pokémon Enhancements) opens the boxes kept on your PC straight away: no GBA is needed to look through them or tidy them up. **Load from cartridge** reads your cartridge's PC boxes over GB-Link and shows them beside the bank, with the box icons and a summary laid out. Three modes at the top:

- **Move:** drag a Pokémon to another slot. A Pokémon already there swaps places. Clicking one only shows its summary: nothing moves without a drag.
- **Move Multi:** drag a box over the slots to choose every Pokémon in it (hold Ctrl to add more), or click them one by one to choose them. Then drag any of them to move them all.
- **Release:** mark Pokémon (by clicking, or with a box), then press Release. It asks first, and Undo brings them back until you save. This is the only action that works by clicking.

**Open a save file…** opens a `.sav` file instead of a cartridge, for example one from a flash cart's SD card or from an emulator. Emerald is recognised by itself; for FireRed and LeafGreen the app asks which one it is. On Save, the file is written (a copy of it from before is kept next to it).

Clicking a Pokémon shows everything about it: nature, ability, held item, IVs, EVs and stats, Hidden Power, moves and PP, where it was met, its ball, OT and IDs, friendship, Pokérus and each of its ribbons.

Nothing changes until you press **Save**. Then the cartridge is checked to be the same one, with the same save, as when its boxes were opened; its save is backed up to your PC; the boxes are written as a new save (the game's previous save stays as its backup) and read back to check them; and only then is the bank on your PC updated. If anything stops part way, the next time you open the bank with that cartridge it works out where every Pokémon is, so none are lost.

The bank is a folder for each box, with a `.pk3` file for each Pokémon. Every save keeps a dated copy of the whole bank in its Backups folder (the last 30). Settings › Pokémon Enhancements has buttons for both folders.

The names, moves, items, places and icons are read from your own game (a ROM in your games list, or the cartridge the first time) and kept on your PC. The icons can be read from an English cartridge, or from any Ruby, Sapphire, Emerald, FireRed or LeafGreen ROM in your games list; without one, the Pokémon show by name. Works with every language of the three games: English, French, German, Italian, Spanish and Japanese (a Japanese save file is recognised by itself. Pokémon move between languages as they do when trading between the games themselves. For a Japanese game, the move, item and place names come from another of your games the bank has read before, as they're numbered the same in every language. The party isn't shown: put a Pokémon in a box in the game to move it.

### Game Boy Pokémon Bank (Red, Blue, Yellow, Gold, Silver, Crystal)
**Pokémon Bank** now asks which bank to open. The **Game Boy Pokémon Bank** is a separate bank for the six Game Boy and Game Boy Color Pokémon games. Pokémon move freely between all six, but never between a Game Boy game and a GBA game: the two banks are kept completely apart.

- **Load from cartridge** reads a cartridge's boxes over GB-Link (the GBA becomes a Game Boy Color for this, as when playing). If the cartridge is a **flash cart showing its own menu**, like the **EZ-Flash Junior**, the save is read from the cart's SRAM, where the EZ-Flash Junior keeps the last game's save until it copies it to the SD card.
- **Open a save file…** opens a `.sav` from a flash cart's SD card or from an emulator (mGBA's clock data at the end of a save is kept as it is).
- Moving works as in the GBA bank: **drag only** (Move and Move Multi), with Release the only clicking action, and Undo until you save.
- Clicking a Pokémon shows its summary: level, DVs (and whether it's shiny), stat experience, stats, moves and PP, held item, OT and ID, and for Gold, Silver and Crystal its friendship, Pokérus and the level it was caught at (Crystal).
- **Between generations it works like the Time Capsule:** a Pokémon from Gold, Silver or Crystal can only go to Red, Blue or Yellow if it's one of the first 151, isn't an egg, and knows only generation 1 moves. Its held item becomes a catch rate, as the Time Capsule does. The app tells you why when one can't go.
- **Reading and writing saves** Every language is supported: the international games (English, French, German, Italian, Spanish), the Japanese ones (9 boxes of 30 in Gold, Silver and Crystal, 8 of 30 in Red, Green, Blue and Yellow) and Korean Gold and Silver. As in the games themselves, Japanese, Korean and international Pokémon can't be traded with each other, so the bank only lets each go back to a game of its own kind; a slot shows **JP** or **KO** for those from Japanese and Korean games.
- **Nothing is lost if it stops part way:** the bank writes a note of what it's about to do first, then writes the cartridge or file and reads it back, and only then updates the bank on your PC. Next time, it finishes or undoes what was left.
- The bank is a folder for each box (`Pokemon Bank GB/Boxes`), with a `.pk1` or `.pk2` file for each Pokémon. Dated backups of the whole bank are kept in its Backups folder.

### Cartridge tools
**Cartridge tools** on the games screen has everything for working on a cartridge directly. GBA cartridges use the GB-Link Companion helper (now as fast as GB-Link Cart Doctor); Game Boy and Game Boy Color cartridges use GB-Link Cart Doctor's own programs, with the GBA switched into Game Boy Color mode.

| Tool | What it does |
|---|---|
| **Back up a GBA cartridge** | Copies the game and its save to your PC. The game is checked against No-Intro's list of good dumps. |
| **Write a save to a GBA cartridge** | Replaces the cartridge's save with a `.sav` file. The cartridge's own save is copied to your PC first. |
| **Write a ROM to a GBA flash cart** | Writes a `.gba` game to a flash or reproduction cart, using FlashGBX's list of cart types (and the CFI and common chip commands for others), then reads it back to check it. |
| **Back up a Game Boy cartridge** | Copies a Game Boy or Game Boy Color game and its save to your PC, checked against the game's header checksums. |
| **Write a save to a Game Boy cartridge** | Replaces the cartridge's save with a `.sav` file, then reads it back to check it. Its own save is copied first. |
| **Write a ROM to a Game Boy flash cart** | Writes a `.gb` or `.gbc` game to a Game Boy flash cart (carts written through the cartridge's write pin, from FlashGBX's list), then checks it. |

Long copies can be cancelled, and the top bar's **Home** button leaves at any point.

---

## What you need

- **Windows 10 or 11** (or Linux, see below)
- A **GB-Link adapter** with current firmware, and a **Game Boy Color link cable** (a GBA link cable is missing pins this needs)
- A **GBA or GBA SP** with the game's cartridge in it (for Game Boy games, the GBA is switched into Game Boy Color mode, which a GBA or GBA SP can do)
- **[mGBA](https://mgba.io)**. Development builds and 0.11 load the app's script by themselves. With 0.10, load it once each time with Tools › Scripting… › File › Load script…
- Your game's **ROM** (`.gba`, `.gb` or `.gbc`), the same game as the cartridge

## How to use

1. Run `GBLinkCompanion.exe`. The first time, it checks that the GB-Link is plugged in, where mGBA is, and which folder your games are in.
2. Choose a game and press **Play**.
3. Switch the GBA on while holding **START + SELECT**.
4. Play and save as usual. When you're done, close mGBA, and your save is copied to the cartridge.

`gblink-companion-native/README.md` has the full guide.

---

## Building from source Windows

The app is written in Go with the [Gio](https://gioui.org) UI toolkit. You need Go 1.24 or newer:

```
cd gblink-companion-native
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GBLinkCompanion.exe .
```

In PowerShell, set the variables first: `$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"`.

The GBA helper program and the mGBA script are built into the app from `gblink-companion-native/assets/`, so you only need the following step if you change the GBA side. Build the helper in `b2s/gba` with devkitARM (or `arm-none-eabi-gcc`); if `gbafix` isn't on your PATH, `b2s/tools/gbafix.py` is used instead (it needs Python 3). Then copy it into the app's assets:

```
make -C b2s/gba
cp b2s/gba/gblink_companion_mb.gba gblink-companion-native/assets/
```

## Building from source Linux

### Linux

Install [Go 1.24 or newer](https://go.dev/dl/), then open a terminal in the repository folder and run:

```bash
cd gblink-companion-native
go build -o GBLinkCompanion .
```

This creates the Linux executable `GBLinkCompanion`. Run it with:

```bash
./GBLinkCompanion
```

#### Building an AppImage

To package the app as an AppImage, build the executable first, then place it in `AppDir/usr/bin/` alongside the required launcher, desktop entry and icon.

With `appimagetool` available, run this command from the repository root:

```bash
ARCH=x86_64 ./aitool/appimagetool AppDir GB-Link-Companion-x86_64.AppImage
```

The AppImage will be created as `GB-Link-Companion-x86_64.AppImage`.

## What's in this repository

| Folder | What it is |
|---|---|
| `gblink-companion-native/` | The app: the window, the GB-Link connection, save syncing, the cartridge tools, the two Pokémon Banks, status screens, Wonder Cards. `assets/` holds the GBA helper, the mGBA script (`gblink_controller.lua`), the Wonder Cards, GB-Link Cart Doctor's programs (`cartdoctor/`), the flash cart types (`flashcarts/`) and the Game Boy Pokémon data (`gen12/`) |
| `gblink-companion-native/test/` | End-to-end tests: the real app against a simulated GB-Link and GBA, and libmgba running the real script |
| `b2s/` | The GBA side, based on Burn2Slot: the program that runs on the GBA (link protocol, save and ROM reading, save writing, status screens), a PC simulator of it, and tools |

---

## Credits

- **The GB-Link team** ([gblink.io](https://gblink.io)) for the GB-Link adapter and their open-source tools, which this app builds on:
  - the adapter's protocol and multiboot (ported from their web tools),
  - [GB-Link Cart Doctor](https://github.com/GB-Link/gb-cart-doctor-web) (GPL-3.0): its Game Boy Color switch and cartridge programs, its fast save and ROM transfers (ported into the GBA helper), its GBA cartridge dumper and its flash cart writing code, all ported to the app,
  - every Wonder Card in the app, from [gblink-wondercards](https://github.com/GB-Link/gblink-wondercards) (GPL-3.0). These include the classic event distributions, Project Wonder's cards (Goppier) and the cards by Blisy, Youpileouf and others that the GB-Link team collected, plus the GB-Link Team's own cards.
- **Wired-Ariel** for [gen3-poke-multiplayer](https://github.com/Wired-Ariel/gen3-poke-multiplayer), which provided reference code and inspiration for using GBA multiboot to run code on a real Game Boy Advance. GB-Link Companion uses this approach to inject and execute Wonder Card code while Pokémon Emerald is running on the GBA.
- **vrodin** for [Burn2Slot](https://github.com/vrodin/Burn2Slot) (MIT). The GBA helper started out as Burn2Slot, including its link handling and cartridge and flash code.
- **kwsch and the [PKHeX](https://github.com/kwsch/PKHeX) contributors** (GPL-3.0): the Game Boy Pokémon Bank's save reading and writing, Pokémon conversions and data tables are ported from PKHeX, and the GBA bank's summaries and met locations follow it.
- **Lesserkuma** for [FlashGBX](https://github.com/lesserkuma/FlashGBX) (GPL-3.0), whose flash cart types the flash cart tools use (through GB-Link Cart Doctor).
- **[pret](https://github.com/pret)** for the pokeruby, pokeemerald and pokefirered decompilations. Their symbol files give the addresses the status screens and Wonder Cards use.
- **[mGBA](https://mgba.io)** by endrift, whose scripting API the app's script runs on.
- **[Spleen](https://github.com/fcambus/spleen)** by Frederic Cambus (BSD-2-Clause), the small font on the GBA's status screens.
- **[No-Intro](https://no-intro.org)** for the GBA DAT used to check ROMs and backups.
- **[Gio](https://gioui.org)** for the UI toolkit.

Pokémon is © Nintendo / Creatures Inc. / GAME FREAK inc. This project isn't affiliated with or endorsed by them, and it doesn't include any game ROMs.

## License

GB-Link Companion is free software, licensed under the **GNU General Public License v3.0** (GPL-3.0). See [`LICENSE`](LICENSE). You can use, copy, share and modify; if you share a changed version, it has to be under the GPL-3.0 too, with its source.

It includes work under other licences, all compatible with the GPL-3.0:

- the GB-Link team's Wonder Cards: GPL-3.0, `gblink-companion-native/assets/wondercards/LICENSE-gblink-wondercards`
- GB-Link Cart Doctor's programs and FlashGBX's cart types: GPL-3.0, `gblink-companion-native/assets/cartdoctor/LICENSE-GB-Link-Cart-Doctor`
- code and data ported from PKHeX: GPL-3.0
- Burn2Slot by vrodin: MIT, `b2s/LICENSE-Burn2Slot`
- Spleen by Frederic Cambus: BSD-2-Clause, `b2s/LICENSE-Spleen`
