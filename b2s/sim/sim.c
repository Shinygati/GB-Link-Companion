/* Native test harness: runs the GBA flasher (gba/main.c + gba/flash.c) on the PC.
 *  - stdin/stdout carry the host's 32-bit words (big-endian) and the replies, like the GB-Link.
 *  - the link is simulated bit by bit: words sent while the GBA isn't listening are lost, and a
 *    re-sync can (on purpose, at random) start listening in the middle of a word.
 *  - the cart is a simulated NOR flash: AMD-style "22XX" with any of Burn2Slot's 6 bus wirings and
 *    6 address sequences, or Intel-style.
 * Usage: sim <amd|intel> <busType> <seqType> <sizeMB> <misalign%> <dumpfile> [seed]
 */
#define _GNU_SOURCE
#include <pthread.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include "../gba/gba.h"
#include "../gba/sio.h"
#include "../gba/ui.h"

int gba_main(void);

/* ------------------------------------------------------------------ flash chip model */
static const u32 SEQS[6][6] = {
    {0x555, 0x2AA, 0x555, 0x555, 0x2AA, 0x555},     {0xAAA, 0x555, 0xAAA, 0xAAA, 0x555, 0xAAA},
    {0xAAA, 0x554, 0xAAA, 0xAAA, 0x554, 0xAAA},     {0x5555, 0x2AAA, 0x5555, 0x5555, 0x2AAA, 0x5555},
    {0xAAAA, 0x5554, 0xAAAA, 0xAAAA, 0x5554, 0xAAAA}, {0x1554, 0xAAA, 0x1554, 0x1554, 0xAAA, 0x1554}};
static const u8 BUS1[] = {15, 7, 14, 6, 13, 5, 12, 4, 0, 8, 1, 9, 2, 10, 3, 11};
static const u8 BUS2[] = {0, 8, 1, 9, 6, 14, 7, 15, 2, 10, 3, 11, 4, 12, 5, 13};
static u16 swapBits(u16 d) { u16 x = (d & 1) ^ ((d >> 1) & 1); return d ^ (x | x << 1); }
static u16 swapTo(const u8 *b, u16 d) { u16 r = 0; for (int i = 0; i < 16; i++) r |= ((d >> i) & 1) << b[i]; return r; }
static u16 swapFrom(const u8 *b, u16 d) { u16 r = 0; for (int i = 0; i < 16; i++) r |= ((d >> b[i]) & 1) << i; return r; }

static int chip_intel, wiring, seqt;
static u32 chip_words;
static u16 *mem;
static u16 chip_id, chip_maker;
static enum { RD, U1, U2, AUTO, PROG, E1, E2, E3, CFI, I_STATUS, I_ID, I_PROG, I_ERASE, I_LOCK } st = RD;
static int busy;              /* reads left while an operation is in progress */
static u16 busy_val;
static long n_prog, n_erase;

/* what the chip decodes from a raw bus value (inverse of the flasher's bus_out for this wiring) */
static u16 decode(u16 raw)
{
    switch (wiring) {
        case 1: return swapBits(raw) & 0xFF;
        case 2: return swapFrom(BUS1, raw) & 0xFF;
        case 3: return swapFrom(BUS2, raw) & 0xFF;
        case 4: return raw & 0xFF;
        case 5: return swapBits(raw) & 0xFF;
        default: return raw & 0xFF;
    }
}
/* raw bus value the chip drives so that the flasher's read_word() sees v */
static u16 encode(u16 v)
{
    switch (wiring) {
        case 1: case 5: return swapBits(v);
        case 2: return swapTo(BUS1, v);
        case 3: return swapTo(BUS2, v);
        default: return v;
    }
}
static u32 log2u(u32 v) { u32 n = 0; while ((1u << n) < v) n++; return n; }
static u16 cfi(u32 a)
{
    switch (a) { case 0x10: return 'Q'; case 0x11: return 'R'; case 0x12: return 'Y';
                 case 0x27: return (u16)log2u(chip_words * 2); case 0x2A: return 6; default: return 0; }
}
static void erase_range(u32 a, u32 words) { a &= ~(words - 1); for (u32 i = 0; i < words && a + i < chip_words; i++) mem[a + i] = 0xFFFF; n_erase++; }

u16 sim_cart_rd(u32 a)
{
    if (busy) { busy--; return busy_val; }
    if (!chip_intel) {
        if (st == AUTO) return a == 0 ? encode(chip_maker) : a == 1 ? encode(chip_id) : (a < chip_words ? mem[a] : 0xFFFF);
        if (st == CFI) return encode(cfi(a));
    } else {
        if (st == I_ID) return a == 0 ? chip_maker : a == 1 ? chip_id : 0;
        if (st == CFI) return cfi(a);
        if (st == I_STATUS || st == I_PROG || st == I_ERASE) return 0x0080;
    }
    if (a < chip_words) return mem[a];
    static int mirror = -1;
    if (mirror < 0) mirror = getenv("SIM_MIRROR") != NULL;
    if (mirror) return mem[a % chip_words];                   /* top address lines not wired */
    return (u16)a;                                            /* past the chip: open bus */
}

void sim_cart_wr(u32 a, u16 raw)
{
    if (!chip_intel) {
        const u32 *s = SEQS[seqt];
        u16 c = decode(raw);
        if (st == PROG) { if (a < chip_words) mem[a] &= raw; n_prog++; busy = 3; busy_val = (u16)~raw; st = RD; return; }
        if (c == 0xF0) { st = RD; return; }
        if (c == 0x98 && a == 0x55 && seqt == 0) { st = CFI; return; }
        switch (st) {
            case RD: case AUTO: case CFI: st = (a == s[0] && c == 0xAA) ? U1 : st == RD ? RD : st; return;
            case U1: st = (a == s[1] && c == 0x55) ? U2 : RD; return;
            case U2:
                if (a != s[2]) { st = RD; return; }
                st = c == 0x90 ? AUTO : c == 0xA0 ? PROG : c == 0x80 ? E1 : RD; return;
            case E1: st = (a == s[3] && c == 0xAA) ? E2 : RD; return;
            case E2: st = (a == s[4] && c == 0x55) ? E3 : RD; return;
            case E3:
                if (c == 0x30) { erase_range(a, 0x10000 /* 128 KB sectors */); busy = 20; busy_val = encode(0x0000); }
                st = RD; return;
            default: st = RD; return;
        }
    } else {
        if (st == I_PROG) { if (a < chip_words) mem[a] &= raw; n_prog++; busy = 3; busy_val = 0; st = I_STATUS; return; }
        if (st == I_ERASE) { if (raw == 0xD0) { erase_range(a, 0x2000); busy = 20; busy_val = 0; } st = I_STATUS; return; }
        if (st == I_LOCK) { st = I_STATUS; return; }
        switch (raw) {
            case 0xFF: case 0xF0: st = RD; return;
            case 0x50: return;
            case 0x90: st = I_ID; return;
            case 0x98: st = CFI; return;
            case 0x40: case 0x10: st = I_PROG; return;
            case 0x20: st = I_ERASE; return;
            case 0x60: st = I_LOCK; return;
            default: return;
        }
    }
}


/* ------------------------------------------------------------------- save chip models */
/* SIM_SAVE: sram32 | flash64 (Panasonic 0x1B32) | flash128 (Macronix 0x09C2) | atmel (0x3D1F)
 *           | eeprom4k | eeprom64k */
static int save_kind;           /* 1 sram, 2 flash64, 3 flash128, 4 atmel, 5 eeprom4k, 6 eeprom64k */
static u8 save_mem[0x20000];
static int fstate, fid_mode, fbank, fprog, f_erase_pre, atmel_left; static u32 atmel_page;
static u8 ee_mem[0x2000];
static u32 ee_ptr; static int ee_reading, ee_busy;

u8 sim_save_rd(u32 a)
{
    a &= 0xFFFF;
    if (save_kind == 1) return save_mem[a & 0x7FFF];
    if (save_kind >= 2 && save_kind <= 4) {
        if (fid_mode && a < 2) {
            u16 id = save_kind == 2 ? 0x1B32 : save_kind == 3 ? 0x09C2 : 0x3D1F;
            return a == 0 ? (u8)id : (u8)(id >> 8);
        }
        return save_mem[(fbank << 16) | a];
    }
    return 0xFF;                       /* EEPROM carts: nothing at 0x0E000000 */
}

void sim_save_wr(u32 a, u8 v)
{
    a &= 0xFFFF;
    if (save_kind == 1) { save_mem[a & 0x7FFF] = v; return; }
    if (save_kind < 2 || save_kind > 4) return;
    if (atmel_left) {                                   /* Atmel page write: 128 bytes */
        if (atmel_left == 128) atmel_page = a;
        save_mem[(fbank << 16) | ((atmel_page & 0xFF80) | (a & 0x7F))] = v;
        if (--atmel_left == 0) fstate = 0;
        return;
    }
    if (fprog) { save_mem[(fbank << 16) | a] &= v; fprog = 0; return; }
    if (fstate == 10) { if (a == 0) fbank = v & 1; fstate = 0; return; }
    if (fstate == 0 && a == 0x5555 && v == 0xAA) { fstate = 1; return; }
    if (fstate == 1 && a == 0x2AAA && v == 0x55) { fstate = 2; return; }
    if (fstate == 2 && a == 0x5555) {
        fstate = 0;
        if (v == 0x90) fid_mode = 1;
        else if (v == 0xF0) fid_mode = 0;
        else if (v == 0x80) f_erase_pre = 1;
        else if (v == 0xA0) { if (save_kind == 4) { atmel_left = 128; atmel_page = 0; fstate = 11; } else fprog = 1; }
        else if (v == 0xB0 && save_kind == 3) fstate = 10;
        else if (v == 0x10 && f_erase_pre) { memset(save_mem, 0xFF, sizeof save_mem); f_erase_pre = 0; }
        return;
    }
    if (fstate == 2 && v == 0x30 && f_erase_pre) {      /* sector erase */
        memset(save_mem + ((fbank << 16) | (a & 0xF000)), 0xFF, 0x1000); f_erase_pre = 0; fstate = 0; return;
    }
    if (v == 0xF0) { fid_mode = 0; fstate = 0; return; }
    fstate = 0;
}
/* Atmel: the first data write of a page decides which page it is */
static void atmel_fix(void) {}

void sim_eeprom_send(const u16 *bits, u32 n)
{
    int abits = save_kind == 5 ? 6 : 14;
    if (save_kind < 5) return;
    if (bits[0] == 1 && bits[1] == 1) {                       /* read request */
        if ((int)n != 3 + abits) { ee_ptr = 0xFFFFFFFF; ee_reading = 1; return; }   /* wrong size: junk */
        u32 a = 0; for (int i = 0; i < abits; i++) a = a << 1 | (bits[2 + i] & 1);
        ee_ptr = (a * 8) % (save_kind == 5 ? 0x200 : 0x2000); ee_reading = 1;
    } else if (bits[0] == 1 && bits[1] == 0) {                /* write */
        if ((int)n != 3 + abits + 64) return;
        u32 a = 0; for (int i = 0; i < abits; i++) a = a << 1 | (bits[2 + i] & 1);
        u32 base = (a * 8) % (save_kind == 5 ? 0x200 : 0x2000);
        for (int byte = 0; byte < 8; byte++) {
            u8 v = 0; for (int i = 0; i < 8; i++) v = (u8)(v << 1 | (bits[2 + abits + byte * 8 + i] & 1));
            ee_mem[base + byte] = v;
        }
        ee_busy = 5;
    }
}
void sim_eeprom_recv(u16 *bits, u32 n)
{
    for (u32 i = 0; i < n; i++) {
        if (i < 4) { bits[i] = 0; continue; }
        if (ee_ptr == 0xFFFFFFFF) { bits[i] = 1; continue; }
        u32 bit = i - 4, byte = ee_ptr + bit / 8;
        bits[i] = (ee_mem[byte] >> (7 - bit % 8)) & 1;
    }
}
u16 sim_eeprom_status(void) { if (ee_busy) { ee_busy--; return 0; } return 1; }

static void save_init(void)
{
    const char *k = getenv("SIM_SAVE");
    if (!k) return;
    save_kind = !strcmp(k, "sram32") ? 1 : !strcmp(k, "flash64") ? 2 : !strcmp(k, "flash128") ? 3 :
                !strcmp(k, "atmel") ? 4 : !strcmp(k, "eeprom4k") ? 5 : !strcmp(k, "eeprom64k") ? 6 : 0;
    for (u32 i = 0; i < sizeof save_mem; i++) save_mem[i] = (u8)(i * 7 + (i >> 8) * 13);
    for (u32 i = 0; i < sizeof ee_mem; i++) ee_mem[i] = (u8)(i * 11 + 5);
    const char *in = getenv("SIM_SAVE_IN");              /* persistent cart save across power cycles */
    if (in) {
        FILE *f = fopen(in, "rb");
        if (f) {
            if (save_kind >= 5) { size_t n = fread(ee_mem, 1, sizeof ee_mem, f); (void)n; }
            else { size_t n = fread(save_mem, 1, sizeof save_mem, f); (void)n; }
            fclose(f);
        }
    }
    (void)atmel_fix;
}
static void save_dump(const char *path)
{
    FILE *f = fopen(path, "wb");
    if (save_kind == 1) fwrite(save_mem, 1, 0x8000, f);
    else if (save_kind == 2 || save_kind == 4) fwrite(save_mem, 1, 0x10000, f);
    else if (save_kind == 3) fwrite(save_mem, 1, 0x20000, f);
    else if (save_kind == 5) fwrite(ee_mem, 1, 0x200, f);
    else if (save_kind == 6) fwrite(ee_mem, 1, 0x2000, f);
    fclose(f);
}

/* ------------------------------------------------------------------- link model */
static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t cv = PTHREAD_COND_INITIALIZER;
static int armed, nbits, completed, gba_waiting, pend_active, pend_words, pend_bit;
static u32 shift_out, shift_in, done_word, pend_val;
static int misalign_pct;
static long n_lost, n_misaligned;

/* SIM_KEYS_FILE: a file holding the buttons "held" on the simulated GBA, as hex (KEYINPUT bits,
 * 1 = pressed). Re-read on every KEYINPUT read so tests can change it while running. */
unsigned short sim_keyinput(void)
{
    const char *p = getenv("SIM_KEYS_FILE");
    unsigned v = 0;
    if (p) { FILE *f = fopen(p, "r"); if (f) { if (fscanf(f, "%x", &v) != 1) v = 0; fclose(f); } }
    return (unsigned short)(~v & 0x3FF);
}

void sio_init(void) {}
void sio_arm(u32 reply)
{
    pthread_mutex_lock(&mu);
    armed = 1; nbits = 0; shift_out = reply; shift_in = 0; pend_active = 0;
    pthread_mutex_unlock(&mu);
}
void sio_resync_arm(u32 reply)
{
    pthread_mutex_lock(&mu);
    armed = 0; pend_active = 1; pend_val = reply;
    pend_words = rand() % 12;                         /* busy for a while: these words are lost */
    pend_bit = (rand() % 100 < misalign_pct) ? 1 + rand() % 31 : 0;
    if (pend_bit) n_misaligned++;
    pthread_mutex_unlock(&mu);
}
int sio_wait(u32 *word, u32 timeout_ms)
{
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += timeout_ms / 1000; ts.tv_nsec += (long)(timeout_ms % 1000) * 1000000;
    if (ts.tv_nsec >= 1000000000) { ts.tv_sec++; ts.tv_nsec -= 1000000000; }
    pthread_mutex_lock(&mu);
    gba_waiting = 1;
    pthread_cond_broadcast(&cv);
    while (!completed) {
        if (timeout_ms) { if (pthread_cond_timedwait(&cv, &mu, &ts)) { gba_waiting = 1; pthread_mutex_unlock(&mu); return 0; } }
        else pthread_cond_wait(&cv, &mu);
    }
    completed = 0; gba_waiting = 0;
    *word = done_word;
    pthread_mutex_unlock(&mu);
    return 1;
}
int sio_recv(u32 *dst, u32 n, u32 ack, int arm_last, u32 *ck, u32 timeout_ms)
{
    for (u32 i = 0; i < n; i++) {
        u32 w;
        if (!sio_wait(&w, timeout_ms)) return 0;
        if (i + 1 < n || arm_last) sio_arm(0xA5000000u | ((ack + i) & 0xFFFFFF));
        dst[i] = w;
        *ck = (((*ck) << 5) | ((*ck) >> 27)) + w;
    }
    return 1;
}

/* one host word in, one reply word out */
static u32 clock_word(u32 hw)
{
    u32 reply = 0;
    pthread_mutex_lock(&mu);
    int arm_at = -1;
    if (pend_active) {                       /* GBA busy / waiting for a gap before listening */
        if (pend_words > 0) pend_words--;
        else { arm_at = pend_bit; pend_active = 0; }
    }
    if (!armed && arm_at < 0) n_lost++;
    for (int b = 0; b < 32; b++) {
        if (arm_at == b) { armed = 1; nbits = 0; shift_out = pend_val; shift_in = 0; }
        u32 out = 1;                         /* line idles high when the GBA isn't listening */
        if (armed) {
            out = shift_out >> 31; shift_out <<= 1;
            shift_in = shift_in << 1 | ((hw >> (31 - b)) & 1);
            if (++nbits == 32) {
                armed = 0; done_word = shift_in; completed = 1; gba_waiting = 0;
                pthread_cond_broadcast(&cv);
                while (!gba_waiting) pthread_cond_wait(&cv, &mu);     /* let the GBA react */
            }
        }
        reply = reply << 1 | out;
    }
    pthread_mutex_unlock(&mu);
    return reply;
}

/* --------------------------------------------------------------------- UI stubs */
static int verbose;
u8 ui_jp;
void ui_init(void) {}
void ui_clear_line(int row) { (void)row; }
void ui_text(int row, int col, int color, const char *s) { (void)col; (void)color; if (verbose && (row == 10 || row == 5)) fprintf(stderr, "[gba] %s\n", s); }
void ui_hex(int row, int col, int color, u32 v, int digits) { (void)row; (void)col; (void)color; (void)v; (void)digits; }
void ui_dec(int row, int col, int color, u32 v) { (void)row; (void)col; (void)color; (void)v; }
void ui_bar(int row, u32 done, u32 total) { (void)row; (void)done; (void)total; }

static void *gba_thread(void *arg) { (void)arg; gba_main(); return NULL; }

int main(int argc, char **argv)
{
    if (argc < 7) { fprintf(stderr, "usage: sim <amd|intel> bus seq sizeMB misalign%% dump [seed]\n"); return 2; }
    chip_intel = !strcmp(argv[1], "intel");
    wiring = atoi(argv[2]); seqt = atoi(argv[3]);
    chip_words = (u32)atoi(argv[4]) << 19;
    misalign_pct = atoi(argv[5]);
    srand(argc > 7 ? atoi(argv[7]) : 1);
    verbose = getenv("SIM_VERBOSE") != NULL;
    chip_id = chip_intel ? 0x8902 : 0x227E;
    chip_maker = chip_intel ? 0x89 : 0x01;
    mem = malloc(chip_words * 2);
    for (u32 i = 0; i < chip_words; i++) mem[i] = (u16)(i * 2654435761u >> 7);   /* "old game" */
    mem[0] = 0x002E; mem[1] = 0xEA00;
    memcpy(&mem[0x50], "SIMGAME TEST" "BSIM", 16);                 /* header title + code */
    mem[2] = 0xFF24; mem[3] = 0x51AE;                                /* start of the Nintendo logo */
    if (getenv("SIM_FILL_FF")) memset(mem, 0xFF, chip_words * 2);   /* an erased flash chip */
    if (getenv("SIM_ROM_FILE")) {                                    /* a given ROM image instead */
        FILE *rf = fopen(getenv("SIM_ROM_FILE"), "rb");
        if (rf) { size_t n = fread(mem, 2, chip_words, rf); (void)n; fclose(rf); }
    }
    save_init();
    if (getenv("SIM_ROMSTR")) memcpy(&mem[0x12345], getenv("SIM_ROMSTR"), strlen(getenv("SIM_ROMSTR")));

    pthread_t t;
    pthread_create(&t, NULL, gba_thread, NULL);
    unsigned char b[4];
    while (fread(b, 1, 4, stdin) == 4) {
        u32 r = clock_word((u32)b[0] << 24 | b[1] << 16 | b[2] << 8 | b[3]);
        unsigned char o[4] = {r >> 24, r >> 16, r >> 8, r};
        fwrite(o, 1, 4, stdout);
        fflush(stdout);
    }
    FILE *f = fopen(argv[6], "wb");
    fwrite(mem, 2, chip_words, f);
    fclose(f);
    if (save_kind) { char p[600]; snprintf(p, sizeof p, "%s.sav", argv[6]); save_dump(p); }
    fprintf(stderr, "sim: programmed %ld words, %ld erases, %ld lost words, %ld misaligned re-syncs\n",
            n_prog, n_erase, n_lost, n_misaligned);
    return 0;
}
