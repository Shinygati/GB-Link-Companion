/* Save backup / restore for the cart's save chip: battery SRAM/FRAM, Flash (64 KB / 128 KB, incl.
 * Atmel page-write chips) and EEPROM (512 B / 8 KB). Works with official carts and repros. */
#include "gba.h"
#include "save.h"
#include "proto.h"
#ifndef B2S_SIM
#include "sio.h"
#define SVC()         sio_service()   /* keep answering the host (the GBA's buttons) during long waits */
#else
#define SVC()         ((void)0)
#endif

#ifndef B2S_SIM
#define SAVE8         ((vu8 *)0x0E000000)
#define EEPROM16      ((vu16 *)0x0DFFFF00)    /* works for 32 MB ROMs too */
#define REG_DMA3SAD   (*(vu32 *)0x040000D4)
#define REG_DMA3DAD   (*(vu32 *)0x040000D8)
#define REG_DMA3CNT   (*(vu32 *)0x040000DC)
#define save_rd(a)    (SAVE8[(a)])
#define save_wr(a, v) (SAVE8[(a)] = (u8)(v))
/* EEPROM sits in wait-state region 2: use the slowest timing (as GB-Link Cart Doctor does) */
static void eeprom_timing(void) { REG_WAITCNT = (REG_WAITCNT & 0xF8FFu) | 0x0300u; }
static void eeprom_send(const u16 *bits, u32 n)
{
    eeprom_timing();
    REG_DMA3SAD = (u32)bits; REG_DMA3DAD = (u32)EEPROM16; REG_DMA3CNT = 0x80000000u | n;
    while (REG_DMA3CNT & 0x80000000u) ;
}
static void eeprom_recv(u16 *bits, u32 n)
{
    eeprom_timing();
    REG_DMA3SAD = (u32)EEPROM16; REG_DMA3DAD = (u32)bits; REG_DMA3CNT = 0x80000000u | n;
    while (REG_DMA3CNT & 0x80000000u) ;
}
static u16 eeprom_status(void) { return EEPROM16[0] & 1; }
#define rom_rd(a)     (((vu16 *)0x08000000)[(a)])
static void delay_loop(u32 n) { while (n--) __asm__ volatile("nop"); }
#else
u8   sim_save_rd(u32 a);
void sim_save_wr(u32 a, u8 v);
void sim_eeprom_send(const u16 *bits, u32 n);
void sim_eeprom_recv(u16 *bits, u32 n);
u16  sim_eeprom_status(void);
u16  sim_cart_rd(u32 a);
#define save_rd(a)    sim_save_rd(a)
#define save_wr(a, v) sim_save_wr((a), (u8)(v))
#define eeprom_send   sim_eeprom_send
#define eeprom_recv   sim_eeprom_recv
#define eeprom_status sim_eeprom_status
#define rom_rd(a)     sim_cart_rd(a)
static void delay_loop(u32 n) { (void)n; }
#endif

static u16 flash_id;
static u8  cur_bank = 0xFF;

u32 save_size(u32 type)
{
    switch (type) {
        case SAVE_SRAM32:   return 0x8000;
        case SAVE_SRAM64:   return 0x10000;
        case SAVE_FLASH64:  return 0x10000;
        case SAVE_FLASH128: return 0x20000;
        case SAVE_EEPROM4K: return 0x200;
        case SAVE_EEPROM64K:return 0x2000;
        default:            return 0;
    }
}

/* ---------------------------------------------------------------------------------- Flash */
static void fcmd(u8 c) { save_wr(0x5555, 0xAA); save_wr(0x2AAA, 0x55); save_wr(0x5555, c); }

static u16 flash_read_id(void)
{
    /* Probing writes to 0x5555/0x2AAA; on an SRAM cart that would change two save bytes, so keep
     * them and put them back if this turns out not to be flash. */
    u8 a = save_rd(0x5555), b = save_rd(0x2AAA), z0 = save_rd(0), z1 = save_rd(1);
    fcmd(0x90);
    delay_loop(20000);
    u16 id = (u16)(save_rd(1) << 8 | save_rd(0));
    fcmd(0xF0);
    save_wr(0x0000, 0xF0);                 /* some chips need the plain reset too */
    delay_loop(20000);
    if (save_rd(0x5555) != a || save_rd(0x2AAA) != b) {   /* it was RAM: undo */
        save_wr(0x5555, a); save_wr(0x2AAA, b); save_wr(0, z0); save_wr(1, z1);
        return 0;
    }
    if (id == (u16)(z1 << 8 | z0)) return 0;              /* reads didn't change: not flash */
    return id;
}

static int flash_is_128k(u16 id) { return id == 0x09C2 || id == 0x1362 || id == 0x09C1; }
static int flash_is_atmel(u16 id) { return id == 0x3D1F; }

static void flash_bank(u32 offset, u32 type)
{
    if (type != SAVE_FLASH128) return;
    u8 bank = (u8)(offset >> 16);
    if (bank == cur_bank) return;
    fcmd(0xB0);
    save_wr(0, bank);
    cur_bank = bank;
}

static int flash_wait(u32 a, u8 want)
{
    for (u32 t = 0; t < 0x200000; t++) { if (save_rd(a) == want) return 0; SVC(); }
    return -1;
}

/* ------------------------------------------------------------------------------- EEPROM */
static u16 bits[96];

static void eeprom_read8(u32 block, u32 abits, u8 *out)
{
    u32 n = 0;
    bits[n++] = 1; bits[n++] = 1;
    for (int i = (int)abits - 1; i >= 0; i--) bits[n++] = (block >> i) & 1;
    bits[n++] = 0;
    eeprom_send(bits, n);
    eeprom_recv(bits, 68);
    for (int byte = 0; byte < 8; byte++) {
        u8 v = 0;
        for (int i = 0; i < 8; i++) v = (u8)(v << 1 | (bits[4 + byte * 8 + i] & 1));
        out[byte] = v;
    }
}

static int eeprom_write8(u32 block, u32 abits, const u8 *in)
{
    u32 n = 0;
    bits[n++] = 1; bits[n++] = 0;
    for (int i = (int)abits - 1; i >= 0; i--) bits[n++] = (block >> i) & 1;
    for (int byte = 0; byte < 8; byte++)
        for (int i = 7; i >= 0; i--) bits[n++] = (in[byte] >> i) & 1;
    bits[n++] = 0;
    eeprom_send(bits, n);
    for (u32 t = 0; t < 0x100000; t++) { if (eeprom_status()) return 0; SVC(); }   /* ~10 ms typical */
    return -1;
}

/* ------------------------------------------------------------------------------ public */
static int rom_has(const char *s, u32 len, u32 rom_words)
{
    /* look for the save library string (e.g. "FLASH1M_V") in the game ROM */
    u16 first = (u16)((u8)s[0] | (u8)s[1] << 8);
    for (u32 i = 0; i < rom_words; i++) {
        if (rom_rd(i) != first) continue;
        u32 k = 2;
        for (; k + 1 < len; k += 2)
            if (rom_rd(i + k / 2) != (u16)((u8)s[k] | (u8)s[k + 1] << 8)) break;
        if (k + 1 >= len) {
            if (len & 1) { if ((rom_rd(i + len / 2) & 0xFF) == (u8)s[len - 1]) return 1; }
            else return 1;
        }
    }
    return 0;
}

static u32 rom_words_guess(void)
{
    /* ROM size: the cart bus mirrors / returns open-bus past the end; compare the header */
    u16 h0 = rom_rd(0x50), h1 = rom_rd(0x51), h2 = rom_rd(0x52);
    for (u32 mb = 1; mb < 32; mb *= 2) {
        u32 w = mb << 19;
        if (rom_rd(w + 0x50) == h0 && rom_rd(w + 0x51) == h1 && rom_rd(w + 0x52) == h2) return w;
    }
    return 32u << 19;
}

u32 save_detect(u32 *flashIdOut)
{
#ifndef B2S_SIM
    REG_WAITCNT = (REG_WAITCNT & ~3u) | 3u;   /* slowest SRAM timing: works with every chip */
#endif
    cur_bank = 0xFF;
    flash_id = flash_read_id();
    *flashIdOut = flash_id;
    if (flash_id) return flash_is_128k(flash_id) ? SAVE_FLASH128 : SAVE_FLASH64;

    u32 words = rom_words_guess();
    if (rom_has("EEPROM_V", 8, words)) {
        /* 512 B or 8 KB? Read in 8 KB mode (14-bit addresses): a 512 B chip gives the same 8 bytes
         * for every block, an 8 KB chip doesn't (same check as GB-Link Cart Doctor). */
        u8 first[8], blk[8];
        eeprom_read8(0, 14, first);
        for (u32 b = 1; b < 256; b++) {
            eeprom_read8(b, 14, blk);
            for (int i = 0; i < 8; i++) if (blk[i] != first[i]) return SAVE_EEPROM64K;
        }
        return SAVE_EEPROM4K;
    }
    if (rom_has("FLASH", 5, words)) return SAVE_SRAM64;   /* flash game on a repro with RAM: 64 KB */
    return SAVE_SRAM32;
}

int save_read(u32 type, u32 offset, u32 nbytes, u8 *out)
{
#ifndef B2S_SIM
    REG_WAITCNT = (REG_WAITCNT & ~3u) | 3u;
#endif
    if (offset + nbytes > save_size(type)) return -1;
    if (type == SAVE_EEPROM4K || type == SAVE_EEPROM64K) {
        u32 abits = type == SAVE_EEPROM4K ? 6 : 14;
        if ((offset | nbytes) & 7) return -1;
        for (u32 i = 0; i < nbytes; i += 8) eeprom_read8((offset + i) >> 3, abits, out + i);
        return 0;
    }
    if (type == SAVE_FLASH128) { cur_bank = 0xFF; }
    for (u32 i = 0; i < nbytes; i++) {
        u32 a = offset + i;
        if (type == SAVE_FLASH128) flash_bank(a, type);
        out[i] = save_rd(a & 0xFFFF);
    }
    return 0;
}

/* returns -1 ok, else the offset of the first byte that didn't verify; *got set */
int save_write(u32 type, u32 offset, u32 nbytes, const u8 *in, u8 *got)
{
#ifndef B2S_SIM
    REG_WAITCNT = (REG_WAITCNT & ~3u) | 3u;
#endif
    if (offset + nbytes > save_size(type)) return -2;
    if (type == SAVE_EEPROM4K || type == SAVE_EEPROM64K) {
        u32 abits = type == SAVE_EEPROM4K ? 6 : 14;
        if ((offset | nbytes) & 7) return -2;
        u8 chk[8];
        for (u32 i = 0; i < nbytes; i += 8) {
            if (eeprom_write8((offset + i) >> 3, abits, in + i)) { *got = 0; return (int)(offset + i); }
            eeprom_read8((offset + i) >> 3, abits, chk);
            for (int k = 0; k < 8; k++) if (chk[k] != in[i + k]) { *got = chk[k]; return (int)(offset + i + k); }
        }
        return -1;
    }
    if (type == SAVE_FLASH64 || type == SAVE_FLASH128) {
        if ((offset | nbytes) & 0xFFF) return -2;           /* whole 4 KB sectors */
        cur_bank = 0xFF;
        if (!flash_id) { u32 dummy; save_detect(&dummy); }
        for (u32 s = offset; s < offset + nbytes; s += 0x1000) {
            flash_bank(s, type);
            u32 base = s & 0xFFFF;
            const u8 *src = in + (s - offset);
            if (flash_is_atmel(flash_id)) {
                for (u32 p = 0; p < 0x1000; p += 128) {             /* 128-byte pages, no erase */
                    fcmd(0xA0);
                    for (u32 k = 0; k < 128; k++) save_wr(base + p + k, src[p + k]);
                    if (flash_wait(base + p + 127, src[p + 127])) { *got = save_rd(base + p + 127); return (int)(s + p + 127); }
                }
            } else {
                fcmd(0x80); save_wr(0x5555, 0xAA); save_wr(0x2AAA, 0x55); save_wr(base, 0x30);
                if (flash_wait(base, 0xFF)) { *got = save_rd(base); return (int)s; }
                for (u32 k = 0; k < 0x1000; k++) {
                    if (src[k] == 0xFF) continue;
                    fcmd(0xA0);
                    save_wr(base + k, src[k]);
                    if (flash_wait(base + k, src[k])) { *got = save_rd(base + k); return (int)(s + k); }
                    SVC();
                }
            }
            for (u32 k = 0; k < 0x1000; k++) {
                if (save_rd(base + k) != src[k]) { *got = save_rd(base + k); return (int)(s + k); }
                if (!(k & 1)) SVC();
            }
        }
        return -1;
    }
    /* SRAM / FRAM */
    for (u32 i = 0; i < nbytes; i++) save_wr(offset + i, in[i]);
    for (u32 i = 0; i < nbytes; i++)
        if (save_rd(offset + i) != in[i]) { *got = save_rd(offset + i); return (int)(offset + i); }
    return -1;
}

void save_title(u32 out[4])            /* game title (12 bytes) + game code (4 bytes) from the header */
{
    for (int i = 0; i < 4; i++) out[i] = (u32)rom_rd(0x50 + i * 2) | (u32)rom_rd(0x51 + i * 2) << 16;
}
