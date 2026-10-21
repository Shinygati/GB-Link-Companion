# Builds burn2slot_mb.gba (multiboot image, <256 KB) with a plain arm-none-eabi toolchain, and
# gblink_sync_mb.gba: the same program with the screens of the mGBA save-sync launcher (-DSYNC_UI).
# (devkitARM also works: make PREFIX=$DEVKITARM/bin/arm-none-eabi-).
PREFIX  ?= arm-none-eabi-
CC      := $(PREFIX)gcc
OBJCOPY := $(PREFIX)objcopy
GBAFIX  ?= gbafix
CFLAGS  := -mcpu=arm7tdmi -mthumb -mthumb-interwork -O2 -Wall -Wextra -ffreestanding -fno-builtin -fno-strict-aliasing -std=gnu11
LDFLAGS := -nostdlib -nostartfiles -T gba.ld -Wl,--no-warn-rwx-segments
SRC     := main.c flash.c save.c sio_gba.c ui.c libc.c status.c
OBJ     := $(SRC:.c=.o) crt0.o
SYNC_OBJ := main_sync.o $(filter-out main.o,$(OBJ))

all: burn2slot_mb.gba gblink_sync_mb.gba

gblink_sync_mb.gba: gblink_sync_mb.elf
	$(OBJCOPY) -O binary $< $@
	$(GBAFIX) $@ -p -tGBLINKSYNC -cGBLS -m01
	@python3 -c "import os,sys; n=os.path.getsize('$@'); print('size', n); sys.exit(n > 0x40000)"

gblink_sync_mb.elf: $(SYNC_OBJ) gba.ld
	$(CC) $(CFLAGS) $(LDFLAGS) -o $@ $(SYNC_OBJ) -lgcc

main_sync.o: main.c *.h
	$(CC) $(CFLAGS) -DSYNC_UI -c $< -o $@

burn2slot_mb.gba: burn2slot_mb.elf
	$(OBJCOPY) -O binary $< $@
	$(GBAFIX) $@ -p -tBURN2SLOT -cB2SE -m01
	@python3 -c "import os,sys; n=os.path.getsize('$@'); print('size', n); sys.exit(n > 0x40000)"

burn2slot_mb.elf: $(OBJ) gba.ld
	$(CC) $(CFLAGS) $(LDFLAGS) -o $@ $(OBJ) -lgcc

%.o: %.c *.h
	$(CC) $(CFLAGS) -c $< -o $@
crt0.o: crt0.s
	$(CC) $(CFLAGS) -c $< -o $@
clean:
	rm -f *.o *.elf *.gba
