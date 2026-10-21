# Status screen test tools

Used by `../status_test.py`, which runs the whole thing on Linux: the app, a fake GB-Link whose
GBA is mGBA running the real helper, and "mGBA on the PC" running the real Lua script.

| File | What it is |
|---|---|
| `mkrom.py` | Builds a pretend Pokémon cart for a version (`BPEE0`, `BPRE1`, `AXVE0`, ...) and a scenario (`wild`, `trainer`, `safari`): names, base stats, trainers, region map data and RAM are where pret's symbol files say the real game keeps them. The pictures are made-up test art. Region map data comes from pret's decompilations: check out pokeruby, pokeemerald and pokefirered side by side and set `PRET` to their folder. |
| `rom.s` / `rom.bin` | The cart's program: copies the prepared RAM in at boot and advances the RNG once a frame. `arm-none-eabi-as -o rom.o rom.s && arm-none-eabi-ld -Ttext=0x08000000 -o rom.elf rom.o && arm-none-eabi-objcopy -O binary rom.elf rom.bin` |
| `runlua.c` | Runs a ROM in libmgba with a Lua script loaded the way mGBA does (emu, console, socket, callbacks; "frame" after every frame). Usage: `runlua ROM SCRIPT FRAMES` (`THROTTLE_US` slows it to real time). |

Building (libmgba 0.10 built with Lua, e.g. `cmake -DUSE_LUA=ON -DBUILD_QT=OFF -DBUILD_SDL=OFF`):

```
gcc -O1 -include mgba/flags.h -o runlua runlua.c -I$MGBA/include -I$MGBA/build/include -L$MGBA/build -lmgba
gcc -O2 -include mgba/flags.h -o ../../../b2s/test/mgba_link ../../../b2s/test/mgba_link.c -I$MGBA/include -I$MGBA/build/include -I$MGBA/src -L$MGBA/build -lmgba
go build -tags testhooks -o /tmp/gblink_app ../..
Xvfb :79 &
DISPLAY=:79 python3 ../status_test.py /tmp/gblink_app shots BPEE0 wild
```
