# GB-Link Companion

An unofficial companion app for the [GB-Link](https://gblink.io) USB adapter. You play your Game Boy Advance games in **mGBA** on your PC with the **save from your real cartridge**, and your **GBA** works as the controller. In the Pokémon games (Ruby, Sapphire, Emerald, FireRed and LeafGreen), the GBA can also be a second screen showing live game info.

> **Not affiliated with GB-Link.** GB-Link Companion is an independent, unofficial program. It isn't made, endorsed or supported by the makers of the GB-Link adapter (gblink.io).

---

## What it does

### Play with your GBA cartridge save file
Pick a game and press **Play**:

1. The app sends a small helper program to the GBA through the link cable (multiboot). Switch on the GBA while holding START + SELECT.
2. It **reads the save file from the cartridge** via GB-Link and opens the game in mGBA with it.
3. **Every time you save in the game**, the new save is copied back to the cartridge, so the cartridge stays up to date.
4. When you close mGBA, the final save goes back to the cartridge.

Before a save from the cartridge replaces the one on your PC, the app keeps a backup copy. Supported save types are battery SRAM/FRAM, flash 64 KB and 128 KB, and EEPROM 512 B and 8 KB.

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
- Shinygati's [pokeemeraldQoL](https://github.com/Shinygati/pokeemeraldQoL) and [pokefireredQoL](https://github.com/Shinygati/pokefireredQoL) (v1.1), built from their `master` branches, are recognised without a `.sym`. Newer builds need one like any other hack.

### Wonder Cards (Emerald, FireRed, LeafGreen)
While you play, **Wonder Cards…** lists every card from the GB-Link team's [gblink-wondercards](https://github.com/GB-Link/gblink-wondercards) for your game. That includes the event distributions (Aurora, Mystic and Eon Tickets, Wish Eggs and more), Project Wonder's cards and the GB-Link Team's own cards.

- **Use now:** the gifts or effects of the ticked cards happen straight away, one after another, without replacing the card your game already has.
- **Receive Wondercard:** the card is saved the way Mystery Gift saves one, for the deliveryman on any Pokémon Center's 2F.
- Cards marked *until reset* keep running until the game is reset. Only one of them can run at a time, and **Replace** switches from one to another.
- Your ticked cards are remembered for each game.

Wonder Cards only work with the original English games, checked against No-Intro's list of good dumps. Custom ROMs and ROM hacks still work with everything else: the save on your cartridge, the controller, and the second screen.

### Back up your cartridges
**Back up a cartridge** copies the game (ROM) and its save to your PC. The ROM is checked against No-Intro's list of good dumps.

---

## What you need

- **Windows 10 or 11**
- A **GB-Link adapter** with current firmware, and a **Game Boy Color link cable** (a GBA link cable is missing pins this needs)
- A **GBA or GBA SP** with the game's cartridge in it
- **[mGBA](https://mgba.io)**. Development builds and 0.11 load the app's script by themselves. With 0.10, load it once each time with Tools › Scripting… › File › Load script…
- Your game's **ROM** (`.gba`), the same game as the cartridge

## How to use

1. Run `GBLinkCompanion.exe`. The first time, it checks that the GB-Link is plugged in, where mGBA is, and which folder your games are in.
2. Choose a game and press **Play**.
3. Switch the GBA on while holding **START + SELECT**.
4. Play and save as usual. When you're done, close mGBA, and your save is copied to the cartridge.

`gblink-sync-native/README.md` has the full guide.

---

## Building from source

The app is written in Go with the [Gio](https://gioui.org) UI toolkit. You need Go 1.24 or newer:

```
cd gblink-sync-native
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o GBLinkCompanion.exe .
```

In PowerShell, set the variables first: `$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"`.

The GBA helper program and the mGBA script are built into the app from `gblink-sync-native/assets/`, so you only need the following step if you change the GBA side. Build the helper in `b2s/gba` with devkitARM (or `arm-none-eabi-gcc`) and `gbafix` on your PATH, then copy it into the app's assets:

```
make -C b2s/gba
cp b2s/gba/gblink_sync_mb.gba gblink-sync-native/assets/
```

## What's in this repository

| Folder | What it is |
|---|---|
| `gblink-sync-native/` | The app: the window, the GB-Link connection, save syncing, backups, status screens, Wonder Cards. `assets/` holds the GBA helper, the mGBA script (`gblink_controller.lua`) and the Wonder Cards |
| `gblink-sync-native/test/` | End-to-end tests: the real app against a simulated GB-Link and GBA, and libmgba running the real script |
| `b2s/` | The GBA side, based on Burn2Slot: the program that runs on the GBA (link protocol, save and ROM reading, save writing, status screens), a PC simulator of it, and tools |

---

## Credits

- **The GB-Link team** ([gblink.io](https://gblink.io)) for the GB-Link adapter and their open-source tools, which this app builds on:
  - the adapter's protocol and multiboot (ported from their web tools),
  - the save reading approach and ROM streaming ideas from [gb-cart-doctor-web](https://github.com/GB-Link/gb-cart-doctor-web),
  - every Wonder Card in the app, from [gblink-wondercards](https://github.com/GB-Link/gblink-wondercards) (GPL-3.0). These include the classic event distributions, Project Wonder's cards (Goppier) and the cards by Blisy, Youpileouf and others that the GB-Link team collected, plus the GB-Link Team's own cards.
- **vrodin** for [Burn2Slot](https://github.com/vrodin/Burn2Slot) (MIT). The GBA helper started out as Burn2Slot, including its link handling and cartridge and flash code.
- **[pret](https://github.com/pret)** for the pokeruby, pokeemerald and pokefirered decompilations. Their symbol files give the addresses the status screens and Wonder Cards use.
- **[mGBA](https://mgba.io)** by endrift, whose scripting API the app's script runs on.
- **[Spleen](https://github.com/fcambus/spleen)** by Frederic Cambus (BSD-2-Clause), the small font on the GBA's status screens.
- **[No-Intro](https://no-intro.org)** for the GBA DAT used to check ROMs and backups.
- **[Gio](https://gioui.org)** for the UI toolkit.

Pokémon is © Nintendo / Creatures Inc. / GAME FREAK inc. This project isn't affiliated with or endorsed by them, and it doesn't include any game ROMs.

## License

GB-Link Companion is free software, licensed under the **GNU General Public License v3.0** (GPL-3.0). See [`LICENSE`](LICENSE). You can use, copy, share and modify; if you share a changed version, it has to be under the GPL-3.0 too, with its source.

It includes work under other licences, all compatible with the GPL-3.0:

- the GB-Link team's Wonder Cards: GPL-3.0, `gblink-sync-native/assets/wondercards/LICENSE-gblink-wondercards`
- Burn2Slot by vrodin: MIT, `b2s/LICENSE-Burn2Slot`
- Spleen by Frederic Cambus: BSD-2-Clause, `b2s/LICENSE-Spleen`
