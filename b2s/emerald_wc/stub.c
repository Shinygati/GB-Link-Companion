/* stub.c - the multiboot program GB-Link Companion sends before Emerald starts.
 *
 * The GBA is switched on holding START + SELECT with the Emerald cartridge in: the BIOS waits for a
 * multiboot program instead of starting the cartridge, and gets this one. It checks that the
 * cartridge is retail English Emerald, puts the payload (payload.c) in spare EWRAM, and starts the
 * game through handoff.S, which keeps that EWRAM and chains the payload onto the game's IRQ
 * handler. The game then runs as usual; its save isn't touched.
 *
 * After gen3-poke-multiplayer's hw/mbstub/main.c (Wired-Ariel, GPL-3.0).
 *
 * The screen says what's happening (plain colours), and so does the link port: the app reads
 * 0xB0000000 | reason while the stub can't go on. */
#include "boot_syms.h"

typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
#define REG_DISPCNT   (*(volatile u16 *)0x04000000)
#define REG_VCOUNT    (*(volatile u16 *)0x04000006)
#define REG_SIOCNT    (*(volatile u16 *)0x04000128)
#define REG_SIODATA32 (*(volatile u32 *)0x04000120)
#define REG_RCNT      (*(volatile u16 *)0x04000134)
#define BG_PALETTE    ((volatile u16 *)0x05000000)
#define RGB(r, g, b)  ((u16)((r) | ((g) << 5) | ((b) << 10)))

enum { WHY_NOT_EMERALD = 1, WHY_OTHER_ROM = 2, WHY_TOO_BIG = 3, WHY_BAD_COPY = 4 };

extern const u8 payload_bin[], payload_bin_end[];
extern const u8 __handoff_start[], __handoff_end[], __handoff_lma[];

static void color(u16 c) { REG_DISPCNT = 0; BG_PALETTE[0] = c; }
static void frame(void) { while (REG_VCOUNT >= 160) ; while (REG_VCOUNT < 160) ; }
static u32 rd(u32 a) { return *(volatile u32 *)a; }

/* say why on screen and on the link port, and stay here (the GBA has to be switched off) */
static void refuse(u16 c, u32 why)
{
    REG_RCNT = 0;
    REG_SIOCNT = 0x1000;                          /* normal mode, 32 bits, the adapter's clock */
    for (;;) {
        color(c);
        if (!(REG_SIOCNT & 0x80)) {               /* answer every word the app sends */
            REG_SIODATA32 = 0xB0000000 | why << 16 | (rd(ADDR_GAMECODE) & 0xFFFF);
            REG_SIOCNT = 0x1080;
        }
    }
}

static void copy32(volatile u32 *d, const u32 *s, u32 n) { for (u32 i = 0; i < (n + 3) / 4; i++) d[i] = s[i]; }

int main(void)
{
    u32 size = (u32)(payload_bin_end - payload_bin);
    if (size > PAYLOAD_LIMIT - PAYLOAD_BASE) refuse(RGB(31, 0, 31), WHY_TOO_BIG);
    color(RGB(31, 31, 0));                        /* yellow: checking the cartridge */
    /* read twice, a few frames apart: a cartridge that's loose reads wrong once */
    for (int tries = 0;; tries++) {
        int emerald = rd(ADDR_GAMECODE) == GAMECODE_BPEE && rd(ADDR_POOL_INTRMAIN) == ADDR_INTR_MAIN &&
                      rd(ADDR_POOL_INTRBUF) == ADDR_INTR_MAIN_BUFFER;
        int same = emerald && rd(ADDR_CHK1) == VAL_CHK1 && rd(ADDR_CHK2) == VAL_CHK2 && rd(ADDR_CHK3) == VAL_CHK3 &&
                   rd(ADDR_CHK4) == VAL_CHK4 && *(volatile u8 *)ADDR_REVISION == 0;
        if (same) break;
        if (tries >= 3) refuse(RGB(31, 0, 0), emerald ? WHY_OTHER_ROM : WHY_NOT_EMERALD);   /* red */
        for (int i = 0; i < 10; i++) frame();
    }
    for (volatile u32 *p = (volatile u32 *)PAYLOAD_BASE; p < (volatile u32 *)EWRAM_END; p++) *p = 0;
    copy32((volatile u32 *)PAYLOAD_BASE, (const u32 *)payload_bin, size);
    copy32((volatile u32 *)HANDOFF_BASE, (const u32 *)__handoff_lma, (u32)(__handoff_end - __handoff_start));
    if (rd(PAYLOAD_BASE + PAYLOAD_OFF_MAGIC) != PAYLOAD_MAGIC) refuse(RGB(0, 31, 31), WHY_BAD_COPY);
    color(RGB(0, 31, 0));                         /* green: starting the game */
    for (int i = 0; i < 20; i++) frame();
    __asm__ volatile ("bx %0" :: "r"(HANDOFF_BASE));   /* ARM code at an even address */
    for (;;) ;
}
