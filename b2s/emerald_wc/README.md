# Wonder Cards on a real GBA (retail English Pokémon Emerald)

The GBA side of GB-Link Companion's **Wonder Cards on your GBA**: `emerald_wc_mb.gba`, a multiboot
program that starts Emerald from the cartridge with a small program of its own running inside the
game, which takes Wonder Cards from the app over the link cable.

| File | What it is |
|---|---|
| `stub.c`, `crt0.S`, `stub.ld` | The multiboot program: checks the cartridge is retail English Emerald (game code, start-up code and four signature instructions), puts the payload in spare EWRAM (0x0203CF80 on) and jumps to `handoff.S`. If the cartridge is wrong it stays on a red screen and answers `0xB0 | why` on the link port. |
| `handoff.S` | Starts Emerald inside `AgbMain`, past the line that clears EWRAM, and chains the payload onto the game's interrupt handler. The save isn't touched. |
| `hook.S` | The payload's entry: every interrupt runs `payload_irq()` first, then the game's handler (unless a link word was all there was). |
| `payload.c` | The link port (normal mode, 32 bits, the GB-Link's clock; taken only in the overworld, never while the game's own link is in use) and the cards: the same as the mGBA script's "Wonder Cards" (add for the deliveryman, or use now where you stand; cards that keep running, and replacing them). |
| `boot_syms.h` | Retail English Emerald's addresses. |

The start-up hand-off and the hook come from [gen3-poke-multiplayer](https://github.com/Wired-Ariel/gen3-poke-multiplayer)
by Wired-Ariel (GPL-3.0), which brings overworld multiplayer to Emerald on real hardware the same way.

Build with `make` (arm-none-eabi-gcc and gbafix), then copy `emerald_wc_mb.gba` into GB-Link
Companion's `assets/`. `../test/mgba_wc.c` runs it in mGBA behind a pretend GB-Link with the
serial interrupt, the cartridge's save and clock (build it against libmgba 0.10:
`gcc -O2 -include mgba/flags.h -o mgba_wc mgba_wc.c -lmgba`).

Link protocol: see the top of `payload.c`.
