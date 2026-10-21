# Burn2Slot for GB-Link

Flash `.gba` ROMs onto writable bootleg ("repro" / "Chis") GBA cartridges using a **[GB-Link](https://gblink.io) USB adapter**, a link cable and the GBA itself. No DS, cart flasher or extra hardware needed.

The same page also:
- **backs up and restores saves** on official carts and repros: battery SRAM/FRAM, flash 64 KB and 128 KB, and EEPROM 512 B and 8 KB,
- **sends any multiboot ROM** to the GBA, like GB-Link's multiboot page.

This is a port of **[Burn2Slot](https://github.com/vrodin/Burn2Slot)** by vrodin, which does the same job on a Nintendo DS through Slot-2. The flashing code is the same; it now runs on the GBA, and the ROM comes over the link cable from a web page.

> ⚠️ **Status:** built and tested against simulated flash chips and in the mGBA emulator, but **not yet tested on real hardware**. Try it with a cart you don't mind re-flashing first, and please report results.

---

## How it works

1. The web page **multiboots** a small flasher program (16 KB) into the GBA's RAM over the link cable, the same way GB-Link's [multiboot page](https://multiboot.gblink.io) sends games.
2. The flasher detects the cart's flash chip with Burn2Slot's routines:
   - Intel-style chips, and AMD-style "22XX" chips,
   - all 6 data-bus wirings and all 6 command-address sequences Burn2Slot knows,
   - the chip size, via a CFI query.
3. The page erases the space the ROM needs, then streams the ROM in 16 KB blocks. Each block is checksummed on the way, written word by word, and read back to verify. A block that arrives damaged is sent again.

The GB-Link stays in its normal **GB Link** (SPI pass-through) mode the whole time, so **no firmware changes are needed**.

---

## Requirements

- **GB-Link USB adapter** with current [GB-Link firmware](https://github.com/GB-Link/GBLink-Firmware). You can update it from the [launcher](https://launcher.gblink.io).
- **Game Boy Color link cable**, the same cable GB-Link's multiboot page needs.
- **A Game Boy Advance or GBA SP.** A DS can't be used: it has no link port.
- **A bootleg cart with a flash chip Burn2Slot supports:**
  - Intel-style: IDs `8902`, `8904`, `887D`, `887E`, `88B0`, other `88xx`, Sharp `B0`, Mitsubishi (maker `1C`).
  - AMD-style: IDs `22xx`, `7E7E`, `0057`, `00F8`. These are the common MSP55LV / S29GL / M29W types on 8–32 MB repro carts.
- **A browser with WebUSB** (Chrome, Edge or other Chromium browsers), **or WebSerial** (Firefox 151+).
- **Your ROM** as a `.gba` file, up to the cart's size (32 MB maximum).

---

## How to use

1. Open the page: either host the `web/` folder (see below) or run `make serve` and open <http://localhost:8080>.
2. **Connect:** plug in the GB-Link and press **Connect USB**.
3. **Start the flasher:**
   1. With the GBA **off**, put the bootleg cart in and connect the link cable.
   2. Press **Send flasher**.
   3. Turn the GBA on **while holding START + SELECT**. This makes the GBA skip the cart and wait for the link cable.
   4. The GBA shows *Burn2Slot for GB-Link*, and the page shows the flash chip's ID, maker and size.
4. **Flash a ROM:** drop in your `.gba` file and press **Flash to cart**. Wait for **Done**, then switch the GBA off and back on to play.

To flash another ROM without restarting, leave the GBA on and use **Already running? Detect**, then **Flash to cart** again.

### Back up / restore a save

This works with **official carts** as well as repros.

1. Start the flasher with the game cart inserted: **Send flasher**, then switch the GBA on holding **START + SELECT**.
2. Press **Detect save**. The page shows the game's title and code and the save chip:

   | Save chip | How it's detected |
   |---|---|
   | Flash 64 KB / 128 KB | From the chip's ID. Atmel page-write chips are supported. |
   | EEPROM 512 B / 8 KB | From the game's `EEPROM_V` save library. The size comes from reading the chip: a 512 B chip repeats every 8 bytes. |
   | SRAM / FRAM | Anything else: 32 KB, or 64 KB for flash-save games patched to RAM on repros. |

   You can override the type in the list.
3. **Back up save (.sav)** downloads the save.
4. **Restore save from .sav…** writes a save file back to the cart and verifies it.
   - The file has to match the save size. Emulator saves with a few extra bytes on the end (for example clock data) are accepted.
   - For EEPROM, the file's size decides whether it's treated as 512 B or 8 KB.

> A completely blank 8 KB EEPROM looks the same as a 512 B one, so it's shown as 512 B. Choose **EEPROM 8 KB** in the list if you know better.
>
> Detecting the save on an SRAM cart briefly writes to two save bytes to check for a flash chip, then puts them back.

### Send a multiboot ROM

The **Send a multiboot ROM** section sends any multiboot `.gba`/`.mb` file (up to 256 KB) to the GBA, the same as GB-Link's multiboot page. GB-Link's own [Cart Doctor](https://cartdoctor.gblink.io) can also dump ROMs and saves.

**Speed:** expect very roughly **10–20 minutes for a 16 MB ROM**. Blocks of empty space (all `FF`) aren't sent at all, so padded ROMs go faster. If the link slips, the page slows it down automatically and carries on.

> Flashing erases the area of the cart the ROM covers. On carts that keep the save in the same flash chip ("batteryless" saves), that save may be lost too. Saves on a battery-backed SRAM chip aren't touched.

---

## Troubleshooting

| Problem | What to try |
|---|---|
| Multiboot never finds the GBA | Use a GBC link cable, push both plugs fully in, and hold START + SELECT from the moment you switch on. |
| `Unknown flash chip (ID 0x…)` | The cart's chip isn't one Burn2Slot supports, or the cart isn't a writable repro. |
| `ROM is bigger than the cart` | The detected chip is smaller than the ROM. |
| `Verify failed at 0x…` | The chip didn't take the data. Re-seat the cart, clean its contacts, and flash again. |
| `The GBA is not responding` | The GBA was switched off or the cable moved. Restart from step 3. |
| `The save file is … bytes, but …` | The `.sav` is for a different save type. Choose the right type in the list, or check you have the right file. |
| `The save didn't verify` | The save chip didn't take the data. Check the cart's battery (SRAM carts) and contacts. |

---

## Hosting

`web/` is a static site with no build step and no server code: put the folder on GitHub Pages or any HTTPS host. WebUSB only works on `https://` pages or `http://localhost`. The flasher binary is embedded in `web/burn2slot_mb.js`, so the folder works on its own.

---

## Building

You need an `arm-none-eabi` GCC (devkitARM or a distro package) and `gbafix` (from devkitPro, or `tools/gbafix` in the pret decomp repos).

```
make                 # gba/burn2slot_mb.gba  ->  web/burn2slot_mb.js
                     # also gba/gblink_companion_mb.gba: the same program with the screens of the
                     # GB-Link Companion (built with -DCOMPANION_UI)
make test            # native simulator tests (needs a C compiler and node)
```

### Project layout

| Path | What it is |
|---|---|
| `gba/flash.c` | Burn2Slot's detection, erase and write routines, ported from `arm9/source/app.cpp` |
| `gba/save.c` | Save chip detection, backup and restore (SRAM/FRAM, flash, EEPROM) |
| `gba/main.c`, `gba/sio_gba.c` | Link protocol (32-bit SIO slave) and command handling |
| `gba/proto.h` | Protocol definition shared with the web page |
| `web/burn2slot.js` | Host side of the protocol (erase, stream, retry, verify) |
| `web/app.js`, `web/index.html` | The page |
| `web/usb_connection.js`, `serial_connection.js`, `multiboot.js` | Unchanged from [gba-multiboot-web](https://github.com/GB-Link/gba-multiboot-web) |
| `sim/`, `test/` | Simulator and tests (see below) |

### Testing

- **`sim/sim.c`** runs the GBA code natively, with:
  - a simulated cart: AMD-style chips in every bus wiring and address sequence, or Intel-style,
  - a bit-level simulated link cable that loses words while the GBA is busy and sometimes re-syncs in the middle of a word on purpose.

  `test/run.js` drives it with the real `web/burn2slot.js` and checks the cart byte for byte. All 36 AMD combinations, Intel chips, odd ROM sizes, a 9 MB ROM and 25 % misaligned re-syncs pass.
- **`test/run_save.js`** round-trips saves through simulated SRAM, Panasonic 64 KB flash, Macronix 128 KB flash (bank switching), Atmel page-write flash, and 512 B / 8 KB EEPROM, and checks that the type is detected correctly.
- **`test/mgba_link.c`** runs the actual GBA binary in mGBA (`libmgba-dev`) and plays the GB-Link on its link port with the adapter's timing. `test/run_mgba.js` checks the real ARM link code, including full 64 KB packets. `test/run_mgba_save.js` restores and backs up a save through mGBA's emulated save chip.
- **`test/run_mgba_keys.js`** checks controller mode (the `KEYS` word the mGBA launcher uses to read the GBA's buttons) on the real binary.
- **`test/run_mgba_rom.js`** dumps a 2 MB test cartridge through the real binary in mGBA (ROM size from open bus, then the ROM in 32 KB chunks) and compares it byte for byte. With `SIM=<misalign%>`, it does the same in the simulator. This uses the `ROM_INFO`/`ROM_READ` commands that the save-sync app's cartridge backup relies on.
- **`test/shots_companion.js`** saves screenshots of GB-Link Companion helper's screens from mGBA, for checking the progress bar and text.
- **`test/browser_test.py`** clicks through the real page in headless Chromium (Playwright), with the USB connection bridged to the simulator.

---

## Differences from the DS version

- **Data source:** the ROM arrives over the link cable instead of from an SD card.
- **Timeouts:** every wait has one, so a faulty cart gives an error instead of hanging.
- **Failed words:** a word that doesn't program is retried once. The DS version erased its sector again, which would also wipe the words already written there.
- **Verify:** every block is read back and verified.
- **Speed:** erased (`FFFF`) words are skipped, and the hot loops run from the GBA's fast internal RAM.

---

## Credits and licences

- **Flashing code:** ported from [Burn2Slot](https://github.com/vrodin/Burn2Slot) by vrodin (MIT License).
- **Connection and multiboot code, and page style:** from [GB-Link Multiboot Web](https://github.com/GB-Link/gba-multiboot-web) (GPL-3.0).
- **EEPROM size detection and EEPROM timing:** approach taken from [GB-Link Cart Doctor](https://github.com/GB-Link/gb-cart-doctor-web) (GPL-3.0).
- **On-screen font:** rendered from DejaVu Sans Mono (Bitstream Vera / DejaVu font licence).
- **This project:** GPL-3.0, because it includes GPL-3.0 code. The ported Burn2Slot parts stay available under MIT.

---

### Status screens (GB-Link Companion)

The helper built for GB-Link Companion (`gblink_companion_mb.gba`) can also show status screens while
you play in mGBA: `CMD_STATUS` (`gba/status.c`, `gba/proto.h`) draws a 40x17 text grid, a region
map and pictures that the PC sends. The PC runs the menus from the GBA's buttons (`KEYS`); the GBA
only draws. Any other command puts the normal screen back first.
The status screen font is [Spleen](https://github.com/fcambus/spleen) 5x8 (BSD-2-Clause,
`LICENSE-Spleen`), converted with `tools/mkfont_small.py`.

