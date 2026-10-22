/* mgba_wc.c - a GBA in mGBA, with a game cartridge, behind a pretend GB-Link adapter: for testing
 * Wonder Cards on a real GBA (../emerald_wc). Same word protocol as mgba_link.c (4 bytes big-endian
 * in on stdin, the GBA's reply out on stdout) and the adapter's timing, plus what a game needs:
 * the serial interrupt when a word has gone through, the cartridge's save and its clock.
 *
 * Usage: mgba_wc MULTIBOOT.gba CART.gba [SAVE.sav] [GAP_US]
 * fd 3 (or $RIG_CTL_FD) takes commands, one a line:
 *   s<path>      save the screen as a PPM            k<hex>   buttons held (mGBA's key bits)
 *   f<n>         run n frames                         r<hex>   print a 32-bit word to stderr
 *   g<us>        gap between words                    w<path>  write the save out
 *   m<k>         the link cable pulled out k bits into a word: the GBA's port stays k bits out of
 *                step (each word split across two) until the GBA stops listening and starts again
 * The multiboot program starts first, as with START + SELECT held; the cartridge is there two frames on. */
#include <fcntl.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <mgba/core/core.h>
#include <mgba/core/log.h>
#include <mgba/core/serialize.h>
#include <stdarg.h>
#include <mgba/core/config.h>
#include <mgba/internal/arm/arm.h>
#include <mgba/internal/gba/gba.h>
#include <mgba/internal/gba/io.h>
#include <mgba/internal/gba/savedata.h>
#include <mgba/internal/gba/cart/gpio.h>
#include <mgba-util/vfs.h>

#define CYC_WORD 554                 /* 33 us at 16.78 MHz */
static int CYC_GAP = 503;
static struct mCore *core;
static struct GBA *gba;
static color_t *video;
static unsigned vw, vh;
static const char *savePath;

static uint64_t now(void) { return gba->timing.globalCycles + (uint64_t)mTimingCurrentTime(&gba->timing); }
static uint32_t watchAddr; static int watchOn;
static int misalign;                 /* bits the GBA's port is out of step (m command) */
static uint32_t pendIn, pendOut;     /* ...the start of its word in, the end of its word out */
static int armed(void);
static void step1(void)
{
    if (misalign && !(gba->memory.io[REG_SIOCNT >> 1] & 0x4080)) misalign = 0;   /* the GBA stopped the port: it counts from the first bit again */
    if (!watchOn) { core->step(core); return; }
    uint32_t pc = gba->cpu->gprs[ARM_PC], v = core->rawRead16(core, watchAddr, -1);
    core->step(core);
    uint32_t nv = core->rawRead16(core, watchAddr, -1);
    if (nv != v) fprintf(stderr, "watch %08x: %04x -> %04x at pc %08x lr %08x cpsr %08x\n", watchAddr, v, nv, pc, gba->cpu->gprs[ARM_LR], gba->cpu->cpsr.packed);
}
static void run_cycles(uint64_t n) { uint64_t end = now() + n, k = 0; while (now() < end || k < n / 8) { step1(); k++; } }
static int armed(void) { return (gba->memory.io[REG_SIOCNT >> 1] & 0x1081) == 0x1080; }   /* (the register: a loaded state restores that) */

static void shot(const char *path)
{
    run_cycles(280896 * 2);
    FILE *f = fopen(path, "wb");
    if (!f) return;
    fprintf(f, "P6\n%u %u\n255\n", vw, vh);
    for (unsigned i = 0; i < vw * vh; i++) {
        unsigned c = video[i];
        unsigned char px[3] = {c & 0xFF, (c >> 8) & 0xFF, (c >> 16) & 0xFF};
        fwrite(px, 1, 3, f);
    }
    fclose(f);
}

static void command(char *line)
{
    switch (line[0]) {
    case 's': shot(line + 1); break;
    case 'k': core->setKeys(core, strtoul(line + 1, NULL, 16)); break;
    case 'f': run_cycles((uint64_t)280896 * atoi(line + 1)); break;
    case 'g': CYC_GAP = atoi(line + 1) * 16777 / 1000; break;
    case 'r': { uint32_t a = strtoul(line + 1, NULL, 16);
                fprintf(stderr, "mem %08x = %08x\n", a, core->busRead32(core, a)); break; }
    case 'c': for (int i = 0; i < (line[1] ? atoi(line + 1) : 16); i++) { fprintf(stderr, "pc %08x lr %08x cpsr %08x\n", gba->cpu->gprs[ARM_PC], gba->cpu->gprs[ARM_LR], gba->cpu->cpsr.packed); run_cycles(3000); } break;
    case 'W': { uint32_t a = strtoul(line + 1, NULL, 16), v = core->rawRead32(core, a, -1), pc = 0;
                for (long i = 0; i < (getenv("WSTEPS") ? atol(getenv("WSTEPS")) : 40000000); i++) {
                    pc = gba->cpu->gprs[ARM_PC]; core->step(core);
                    uint32_t nv = core->rawRead32(core, a, -1);
                    if (nv != v) { fprintf(stderr, "watch %08x: %08x -> %08x at pc %08x lr %08x (step %ld)\n", a, v, nv, pc, gba->cpu->gprs[ARM_LR], i); v = nv; }
                } break; }
    case 'S': { struct VFile *vf = VFileOpen(line + 1, O_WRONLY | O_CREAT | O_TRUNC);
                if (vf) { mCoreSaveStateNamed(core, vf, SAVESTATE_SAVEDATA | SAVESTATE_RTC); vf->close(vf); } break; }
    case 'L': { struct VFile *vf = VFileOpen(line + 1, O_RDONLY);
                if (vf) { mCoreLoadStateNamed(core, vf, SAVESTATE_SAVEDATA | SAVESTATE_RTC); vf->close(vf); }
                gba->sio.siocnt = gba->memory.io[REG_SIOCNT >> 1];   /* not restored by itself */
                break; }
    case 'P': { uint32_t a = strtoul(line + 1, NULL, 16); long hits = 0;   /* how often the CPU gets to an address */
                for (long i = 0; i < 3000000; i++) { if (gba->cpu->gprs[ARM_PC] - (gba->cpu->cpsr.t ? 4 : 8) == a || gba->cpu->gprs[ARM_PC] == a) hits++; core->step(core); }
                fprintf(stderr, "hits %08x: %ld\n", a, hits); break; }
    case 'i': fprintf(stderr, "sio siocnt %04x io %04x mode %d\n", gba->sio.siocnt, gba->memory.io[REG_SIOCNT >> 1], gba->sio.mode); break;
    case 'm': misalign = atoi(line + 1); pendIn = 0;
              pendOut = (uint32_t)gba->memory.io[REG_SIODATA32_HI >> 1] << 16 | gba->memory.io[REG_SIODATA32_LO >> 1]; break;
    case 'X': watchAddr = strtoul(line + 1, NULL, 16); watchOn = watchAddr != 0; break;
    case 'w': { struct VFile *vf = VFileOpen(line + 1, O_WRONLY | O_CREAT | O_TRUNC);
                if (vf) { GBASavedataClone(&gba->memory.savedata, vf); vf->close(vf); } break; }
    }
    fprintf(stderr, "done %c\n", line[0]);
}

static void quiet(struct mLogger *l, int cat, enum mLogLevel lvl, const char *fmt, va_list a) { (void)l; (void)cat; (void)lvl; (void)fmt; (void)a; }

int main(int argc, char **argv)
{
    static struct mLogger logger = { .log = quiet };
    mLogSetDefaultLogger(&logger);
    if (argc < 3) { fprintf(stderr, "usage: mgba_wc MB CART [SAVE] [GAP_US]\n"); return 2; }
    if (argc > 4) CYC_GAP = atoi(argv[4]) * 16777 / 1000;
    savePath = argc > 3 && argv[3][0] ? argv[3] : NULL;
    int out = dup(1); dup2(2, 1);                  /* mGBA logs to stdout: keep replies apart */
    core = mCoreFind(argv[2]);
    core->init(core);
    mCoreInitConfig(core, NULL);
    core->desiredVideoDimensions(core, &vw, &vh);
    video = calloc(vw * vh, sizeof(color_t));
    core->setVideoBuffer(core, video, vw);
    int nostub = !strcmp(argv[1], "-");               /* "-": the cartridge on its own, for comparing */
    if (nostub) {
        if (!core->loadROM(core, VFileOpen(argv[2], O_RDONLY))) { fprintf(stderr, "cart load failed\n"); return 1; }
    } else if (!GBALoadMB((struct GBA *)core->board, VFileOpen(argv[1], O_RDONLY))) { fprintf(stderr, "multiboot load failed\n"); return 1; }
    core->reset(core);
    gba = core->board;
    gba->idleOptimization = IDLE_LOOP_IGNORE;
    gba->idleLoop = 0xFFFFFFFF;
    run_cycles(280896 * 2);                       /* the multiboot program starts */
    struct VFile *cart = nostub ? NULL : VFileOpen(argv[2], O_RDONLY);   /* then the cartridge is there */
    if (!nostub && (!cart || !GBALoadROM(gba, cart))) { fprintf(stderr, "cart load failed\n"); return 1; }
    if (!nostub) GBASavedataForceType(&gba->memory.savedata, SAVEDATA_FLASH1M);   /* Emerald's 128 KB flash */
    if (savePath) {
        struct VFile *sv = VFileOpen(savePath, O_RDWR);
        if (!sv || !GBALoadSave(gba, sv)) fprintf(stderr, "save load failed\n");
    }
    GBAHardwareInitRTC(&gba->memory.hw);           /* Pokemon's cartridge clock */
    core->rtc.override = RTC_FIXED;
    core->rtc.value = 1761000000LL * 1000;
    gba->idleOptimization = IDLE_LOOP_IGNORE;
    gba->idleLoop = 0xFFFFFFFF;

    int ctl = getenv("RIG_CTL_FD") ? atoi(getenv("RIG_CTL_FD")) : 3;   /* the commands */
    long words = 0, lost = 0;
    static char cb[4096]; static int cn;
    for (;;) {
        struct pollfd pc = {ctl, POLLIN, 0};
        if (poll(&pc, 1, 0) > 0) {
            int n = read(ctl, cb + cn, sizeof cb - 1 - cn);
            if (n > 0) {
                cn += n; cb[cn] = 0;
                char *line = cb, *nl;
                while ((nl = strchr(line, '\n'))) { *nl = 0; command(line); line = nl + 1; }
                cn = (int)strlen(line); memmove(cb, line, cn + 1);
                continue;
            }
        }
        struct pollfd p = {0, POLLIN, 0};
        if (poll(&p, 1, 0) <= 0) { run_cycles(2000); continue; }
        unsigned char b[4];
        if (read(0, b, 4) != 4) break;
        uint32_t hw = (uint32_t)b[0] << 24 | b[1] << 16 | b[2] << 8 | b[3], reply = 0xFFFFFFFF;
        words++;
        if (misalign && armed()) {                   /* out of step: this word ends the GBA's last one */
            int k = misalign;
            uint16_t *io = gba->memory.io;
            uint32_t got = pendIn << k | hw >> (32 - k);
            io[REG_SIODATA32_LO >> 1] = got & 0xFFFF;
            io[REG_SIODATA32_HI >> 1] = got >> 16;
            io[REG_SIOCNT >> 1] &= ~0x0080;
            gba->sio.siocnt &= ~0x0080;
            if (io[REG_SIOCNT >> 1] & 0x4000) GBARaiseIRQ(gba, GBA_IRQ_SIO, 0);
            run_cycles(CYC_WORD);                     /* the GBA answers and listens again */
            uint32_t next = (uint32_t)io[REG_SIODATA32_HI >> 1] << 16 | io[REG_SIODATA32_LO >> 1];
            if (misalign) {
                reply = pendOut << (32 - k) | next >> k;
                pendIn = hw & ((1u << (32 - k)) - 1);
                pendOut = next & ((1u << k) - 1);
            }
        } else if (armed()) {
            run_cycles(CYC_WORD);
            if (armed()) {                            /* still listening after the word's 33 us */
                uint16_t *io = gba->memory.io;
                reply = (uint32_t)io[REG_SIODATA32_HI >> 1] << 16 | io[REG_SIODATA32_LO >> 1];
                io[REG_SIODATA32_LO >> 1] = hw & 0xFFFF;
                io[REG_SIODATA32_HI >> 1] = hw >> 16;
                io[REG_SIOCNT >> 1] &= ~0x0080;
                gba->sio.siocnt &= ~0x0080;
                if (io[REG_SIOCNT >> 1] & 0x4000) GBARaiseIRQ(gba, GBA_IRQ_SIO, 0);
            } else lost++;
        } else {
            lost++;
            run_cycles(CYC_WORD);
        }
        run_cycles(CYC_GAP);
        unsigned char o[4] = {reply >> 24, reply >> 16, reply >> 8, reply};
        if (write(out, o, 4) != 4) break;
    }
    fprintf(stderr, "mgba_wc: %ld words, %ld lost, PC=%08x\n", words, lost, gba->cpu->gprs[ARM_PC]);
    return 0;
}
