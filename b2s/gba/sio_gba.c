/* Link cable as a 32-bit normal-mode SIO slave; the GB-Link adapter is the clock master. */
#include "gba.h"
#include "sio.h"
#include "proto.h"

#define SIO_32BIT     0x1000
#define SIO_START     0x0080
#define RCNT_GPIO     0x8000          /* general-purpose mode: bit 0 reads the SC (clock) line */
#define IDLE_CYCLES   252             /* 15 us at 16.78 MHz: longer than any gap inside a word */
#define TICKS_PER_MS  16              /* timer 2 at 16.78 MHz / 1024 = 16.4 kHz */

void sio_init(void)
{
    REG_IME = 0;                      /* no interrupts: everything is polled */
    REG_TM0CNT_H = 0; REG_TM0CNT_L = 0; REG_TM0CNT_H = 0x0080;          /* free-running, 1 cycle   */
    REG_TM2CNT_H = 0; REG_TM2CNT_L = 0; REG_TM2CNT_H = 0x0083;          /* 1024 cycles per tick    */
    REG_TM3CNT_H = 0; REG_TM3CNT_L = 0; REG_TM3CNT_H = 0x0084;          /* cascade: high 16 bits   */
}

IWRAM_CODE static u32 ticks(void)
{
    u32 hi, lo;
    do { hi = REG_TM3CNT_L; lo = REG_TM2CNT_L; } while (hi != REG_TM3CNT_L);
    return hi << 16 | lo;
}

IWRAM_CODE void sio_arm(u32 reply)
{
    REG_SIOCNT = SIO_32BIT;
    REG_SIODATA32 = reply;
    REG_SIOCNT = SIO_32BIT | SIO_START;
}

IWRAM_CODE void sio_resync_arm(u32 reply)
{
    /* Watch the clock line until it hasn't changed for 15 us, i.e. we're in the gap between two
     * words, then start listening - so we never start counting bits in the middle of a word. */
    REG_SIOCNT = SIO_32BIT;
    REG_RCNT = RCNT_GPIO;
    u16 level = REG_RCNT & 1, start = REG_TM0CNT_L;
    for (;;) {
        u16 now = REG_RCNT & 1;
        if (now != level) { level = now; start = REG_TM0CNT_L; }
        else if ((u16)(REG_TM0CNT_L - start) > IDLE_CYCLES) break;
    }
    REG_RCNT = 0;
    sio_arm(reply);
}

IWRAM_CODE int sio_wait(u32 *word, u32 timeout_ms)
{
    if (timeout_ms) {
        u32 t0 = ticks(), lim = timeout_ms * TICKS_PER_MS;
        while (REG_SIOCNT & SIO_START)
            if (ticks() - t0 > lim) return 0;
    } else {
        while (REG_SIOCNT & SIO_START) ;
    }
    *word = REG_SIODATA32;
    return 1;
}

IWRAM_CODE int sio_recv(u32 *dst, u32 n, u32 ack, int arm_last, u32 *ck, u32 timeout_ms)
{
    u32 c = *ck, lim = timeout_ms * TICKS_PER_MS;
    for (u32 i = 0; i < n; i++) {
        u32 t0 = ticks();
        while (REG_SIOCNT & SIO_START)
            if (ticks() - t0 > lim) { *ck = c; return 0; }
        u32 w = REG_SIODATA32;
        if (i + 1 < n || arm_last) {                 /* re-arm first, then do the bookkeeping */
            REG_SIODATA32 = W_ACK(ack + i);
            REG_SIOCNT = SIO_32BIT | SIO_START;
        }
        dst[i] = w;
        c = CK_STEP(c, w);
    }
    *ck = c;
    return 1;
}
