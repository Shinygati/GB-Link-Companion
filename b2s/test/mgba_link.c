/* Runs the real GBA flasher binary in mGBA and plays the GB-Link adapter on its link port.
 * Same stdin/stdout word protocol as sim/sim.c, so test/run_mgba.js can drive it with the real
 * web host code. Timing follows the adapter: ~33 us to clock a word + GAP_US between words;
 * a word that arrives while the GBA isn't listening is lost (reply 0xFFFFFFFF).
 * The cart here is mGBA's read-only ROM, so this tests the link code, not flashing. */
#include <fcntl.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <stdlib.h>
#include <unistd.h>
#include <mgba/core/core.h>
#include <mgba/internal/gba/cart/gpio.h>
#include <mgba/core/config.h>
#include <mgba/internal/arm/arm.h>
#include <mgba/internal/gba/gba.h>
#include <mgba/internal/gba/io.h>
#include <mgba-util/vfs.h>

#define CYC_WORD  554    /* 33 us at 16.78 MHz */
static int CYC_GAP = 503; /* 30 us */

static struct mCore *core;
static struct GBA *gba;
static uint64_t now(void) { return gba->timing.globalCycles + (uint64_t)mTimingCurrentTime(&gba->timing); }
/* run the GBA for n cycles. (mGBA's cycle counter can jump ahead when it handles an event, which
 * used to starve the CPU of instructions; so also run at least n/8 instructions.) */
static void run_cycles(uint64_t n) { uint64_t end = now() + n, k = 0; while (now() < end || k < n / 8) { core->step(core); k++; } }
static color_t *video;
static unsigned vw, vh;
/* "s<path>" on fd 3: save the GBA screen as a PPM (for checking what the player sees) */
static void run_cycles(uint64_t n);
static void shot(const char *path)
{
    fprintf(stderr, "shot at cycle %llu\n", (unsigned long long)now());
    run_cycles(280896 * 2);                       /* two whole frames, so the picture is current */
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
static uint32_t ring[4096][3];
static int stall_reset;
static long ringn;
static int armed(void) { return (gba->sio.siocnt & 0x1081) == 0x1080; }

int main(int argc, char **argv)
{
    if (argc > 2) CYC_GAP = atoi(argv[2]) * 16777 / 1000;
    int out = dup(1); dup2(2, 1);                  /* mGBA logs to stdout: keep replies separate */
    core = mCoreFind(argv[1]);
    core->init(core);
    mCoreInitConfig(core, NULL);
    core->desiredVideoDimensions(core, &vw, &vh);
    video = calloc(vw * vh, sizeof(color_t));
    core->setVideoBuffer(core, video, vw);
    struct VFile *vf = VFileOpen(argv[1], O_RDONLY);
    if (!GBALoadMB((struct GBA *)core->board, vf)) { fprintf(stderr, "load failed\n"); return 1; }
    core->reset(core);
    gba = core->board;
    if (getenv("MGBA_KEYS")) core->setKeys(core, strtoul(getenv("MGBA_KEYS"), NULL, 16));   /* buttons held */
    gba->idleOptimization = IDLE_LOOP_IGNORE;     /* no fast-forwarding of polling loops */
    gba->idleLoop = 0xFFFFFFFF;
    run_cycles(280896 * 2);                       /* two frames to boot */
    if (getenv("MGBA_CART")) {                    /* a game cartridge in the slot, for ROM dumps */
        struct VFile *cv = VFileOpen(getenv("MGBA_CART"), O_RDONLY);   /* (inserted after boot, so */
        if (!cv || !GBALoadROM(gba, cv)) { fprintf(stderr, "cart load failed\n"); return 1; }  /* the BIOS runs the helper) */
        if (getenv("MGBA_CART_RTC")) {            /* the cart has a clock (Pokemon RSE), stopped at this Unix time */
            GBAHardwareInitRTC(&gba->memory.hw);
            core->rtc.override = RTC_FIXED;
            core->rtc.value = strtoll(getenv("MGBA_CART_RTC"), NULL, 10) * 1000LL;
        }
    }

    long lost = 0, words = 0; int xlog = 0;
    unsigned char b[4];
    for (;;) {
        struct pollfd pc = {3, POLLIN, 0};           /* timing changes from the host (fd 3) */
        if (poll(&pc, 1, 0) > 0) {                   /* one command a line: s<path>, k<hex>, or a gap in us */
            static char cb[1024]; static int cn;
            int n = read(3, cb + cn, sizeof cb - 1 - cn);
            if (n > 0) {
                cn += n; cb[cn] = 0;
                char *line = cb, *nl;
                while ((nl = strchr(line, '\n'))) {
                    *nl = 0;
                    if (line[0] == 's') shot(line + 1);
                    else if (line[0] == 'k') core->setKeys(core, strtoul(line + 1, NULL, 16));   /* buttons held */
                    else { int us = atoi(line); if (us > 0) { CYC_GAP = us * 16777 / 1000; fprintf(stderr, "mgba_link: gap now %d us\n", us); } }
                    line = nl + 1;
                }
                cn = (int)strlen(line); memmove(cb, line, cn + 1);
                continue;
            }
        }
        struct pollfd p = {0, POLLIN, 0};
        if (poll(&p, 1, 0) <= 0) { run_cycles(2000); continue; }   /* host idle (USB latency) */
        if (read(0, b, 4) != 4) break;
        uint32_t hw = (uint32_t)b[0] << 24 | b[1] << 16 | b[2] << 8 | b[3], reply = 0xFFFFFFFF;
        words++;
        if (armed()) {
            if (getenv("STALL")) stall_reset = 1;
            run_cycles(CYC_WORD);                     /* clocking the word */
            uint16_t *io = gba->memory.io;
            reply = (uint32_t)io[REG_SIODATA32_HI >> 1] << 16 | io[REG_SIODATA32_LO >> 1];
            io[REG_SIODATA32_LO >> 1] = hw & 0xFFFF;
            io[REG_SIODATA32_HI >> 1] = hw >> 16;
            io[REG_SIOCNT >> 1] &= ~0x0080;
            gba->sio.siocnt &= ~0x0080;
            if (getenv("TRACE")) {               /* how long until the GBA listens again? */
                uint64_t t = now(); uint32_t pcs[64]; int np = 0; uint64_t next = t;
                static int dumped = 0;
                while (!armed() && now() - t < 4000) {
                    if (now() >= next && np < 64) { pcs[np++] = ((struct ARMCore *)core->cpu)->gprs[15]; next += 40; }
                    uint64_t c0 = now(); uint32_t pc = ((struct ARMCore *)core->cpu)->gprs[15];
                    core->step(core);
                    if (getenv("STEPS") && dumped < 1 && words > 20) fprintf(stderr, "  %08x %llu\n", pc, (unsigned long long)(now() - c0));
                }
                if (words > 20) dumped++;
                uint64_t d = now() - t;
                static uint64_t hist[8]; hist[d < 100 ? 0 : d < 200 ? 1 : d < 300 ? 2 : d < 400 ? 3 : d < 503 ? 4 : d < 4000 ? 5 : 6]++;
                if (getenv("HIST") && words % 20000 == 0) fprintf(stderr, "re-arm latency: <100:%llu <200:%llu <300:%llu <400:%llu <503:%llu  >503:%llu busy:%llu\n", (unsigned long long)hist[0],(unsigned long long)hist[1],(unsigned long long)hist[2],(unsigned long long)hist[3],(unsigned long long)hist[4],(unsigned long long)hist[5],(unsigned long long)hist[6]);
                if (d > 503 && d < 4000 && getenv("SLOW")) { fprintf(stderr, "word %ld re-armed after %llu cycles:", words, (unsigned long long)d);
                               for (int i = 0; i < np; i++) fprintf(stderr, " %08x", pcs[i]); fprintf(stderr, "\n"); }
                run_cycles(CYC_GAP > (int)d ? CYC_GAP - d : 0);
                unsigned char o[4] = {reply >> 24, reply >> 16, reply >> 8, reply};
                if (write(out, o, 4) != 4) break;
                continue;
            }
        } else {
            lost++;
            if (getenv("STALL")) {
                static int deaf = 0;
                if (stall_reset) { deaf = 0; stall_reset = 0; }
                uint32_t pc = ((struct ARMCore *)core->cpu)->gprs[15];
                deaf++;
                if (deaf == 20000) {
                    uint32_t last = 0; int shown = 0;
                    for (int k = 0; k < 3000 && shown < 60; k++) {
                        uint32_t p2 = ((struct ARMCore *)core->cpu)->gprs[15];
                        if (p2 != last) { fprintf(stderr, " %08x", p2); last = p2; shown++; }
                        core->step(core);
                    }
                    fprintf(stderr, "\n TM2=%04x TM3=%04x TM2CNT=%04x TM3CNT=%04x r4=%08x r6=%08x\n", core->busRead16(core, 0x04000108), core->busRead16(core, 0x0400010C),
                            core->busRead16(core, 0x0400010A), core->busRead16(core, 0x0400010E), ((struct ARMCore *)core->cpu)->gprs[4], ((struct ARMCore *)core->cpu)->gprs[6]);
                }
                if (deaf == 20000) fprintf(stderr, "STALL: io=%04x siocnt=%04x bus=%04x rcnt=%04x pc=%08x r3=%08x cpsr=%08x\n",
                    gba->memory.io[REG_SIOCNT >> 1], gba->sio.siocnt, core->busRead16(core, 0x04000128),
                    gba->memory.io[REG_RCNT >> 1], pc, ((struct ARMCore *)core->cpu)->gprs[3], ((struct ARMCore *)core->cpu)->cpsr.packed);
            }
            if (getenv("DBG") && lost < 20) {
                uint64_t t = now(); int n = 0; while (!armed() && n < 200000) { core->step(core); n++; }
                fprintf(stderr, "lost word %ld: siocnt=%04x rcnt=%04x, GBA armed again after %llu cycles, PC=%08x\n", words,
                        gba->memory.io[REG_SIOCNT >> 1], gba->memory.io[REG_RCNT >> 1], (unsigned long long)(now() - t), ((struct ARMCore *)core->cpu)->gprs[15]);
            }
            run_cycles(CYC_WORD);
        }
        run_cycles(CYC_GAP);
        if (getenv("XLOG") && (hw >> 24) == 0x42) xlog = 12;
        if (getenv("RLOG") && (reply >> 24) == 0x52) fprintf(stderr, "  word %ld: reply %08x\n", words, reply);
        if (xlog > 0) { xlog--; fprintf(stderr, "  word %ld: host %08x -> reply %08x\n", words, hw, reply); }
        if (getenv("RING")) { ring[ringn % 4096][0] = hw; ring[ringn % 4096][1] = reply; ring[ringn % 4096][2] = ((struct ARMCore *)core->cpu)->gprs[15]; ringn++; }
        unsigned char o[4] = {reply >> 24, reply >> 16, reply >> 8, reply};
        if (write(out, o, 4) != 4) break;
    }
    if (getenv("RING")) {                         /* the last words before the host gave up */
        long from = ringn > atol(getenv("RING")) ? ringn - atol(getenv("RING")) : 0;
        for (long i = from; i < ringn && i < from + 4096; i++) fprintf(stderr, "  ring %ld: host %08x -> reply %08x  pc %08x\n", i, ring[i % 4096][0], ring[i % 4096][1], ring[i % 4096][2]);
    }
    fprintf(stderr, "mgba_link: at the end SIOCNT=%04x (sio.siocnt=%04x, bus %04x) RCNT=%04x IME=%04x IE=%04x IF=%04x r2=%08x\n", gba->memory.io[REG_SIOCNT >> 1], gba->sio.siocnt, core->busRead16(core, 0x04000128), gba->memory.io[REG_RCNT >> 1], core->busRead16(core,0x04000208), core->busRead16(core,0x04000200), core->busRead16(core,0x04000202), ((struct ARMCore *)core->cpu)->gprs[2]);
    fprintf(stderr, "mgba_link: %ld words, %ld arrived while the GBA wasn't listening, PC=%08x\n",
            words, lost, ((struct ARMCore *)core->cpu)->gprs[15]);
    return 0;
}
