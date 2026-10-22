# Wonder Cards on a real GBA for GB-Link Companion: emerald_wc_mb.gba, a multiboot program that
# starts retail English Pokemon Emerald with payload.c running inside it.
# Needs arm-none-eabi-gcc and gbafix (devkitARM works too: make PREFIX=$DEVKITARM/bin/arm-none-eabi-).
PREFIX  ?= arm-none-eabi-
CC      := $(PREFIX)gcc
OBJCOPY := $(PREFIX)objcopy
GBAFIX  ?= gbafix
CFLAGS  := -mcpu=arm7tdmi -mthumb -mthumb-interwork -Os -Wall -Wextra -ffreestanding -fno-builtin -std=gnu11 -fno-strict-aliasing

all: emerald_wc_mb.gba

payload.elf: hook.S payload.c payload.ld
	$(CC) $(CFLAGS) -nostdlib -nostartfiles -T payload.ld -Wl,--no-warn-rwx-segments -o $@ hook.S payload.c -lgcc
	@python3 -c "import subprocess,sys; o=subprocess.run(['$(PREFIX)nm','payload.elf'],capture_output=True,text=True).stdout; e=[int(l.split()[0],16) for l in o.splitlines() if l.endswith(' payload_end')][0]; print('payload ends at %08x' % e); sys.exit(e > 0x0203FC00)"

payload.bin: payload.elf
	$(OBJCOPY) -O binary $< $@

emerald_wc_mb.elf: crt0.S stub.c handoff.S payload_blob.S payload.bin stub.ld boot_syms.h
	$(CC) $(CFLAGS) -nostdlib -nostartfiles -T stub.ld -Wl,--no-warn-rwx-segments -o $@ crt0.S stub.c handoff.S payload_blob.S -lgcc

emerald_wc_mb.gba: emerald_wc_mb.elf
	$(OBJCOPY) -O binary $< $@
	$(GBAFIX) $@ -p -tGBLINKWC -cGBWC -m01
	@python3 -c "import os,sys; n=os.path.getsize('$@'); print('size', n); sys.exit(n > 0x3FF40)"

clean:
	rm -f *.o *.elf *.bin *.gba
