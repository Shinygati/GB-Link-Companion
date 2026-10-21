#ifndef SIO_H
#define SIO_H
#include "gba.h"
/* Link cable as a 32-bit normal-mode SIO slave (the GB-Link adapter drives the clock). */
void sio_init(void);
/* wait until the clock line has been idle (between words), then load `reply` and listen */
void sio_resync_arm(u32 reply);
/* load `reply` for the next transfer and listen (call right after a transfer completes) */
void sio_arm(u32 reply);
/* wait for a transfer to finish; timeout_ms 0 = forever. Returns 1 and the received word, or 0 */
int  sio_wait(u32 *word, u32 timeout_ms);
/* receive n words into dst, loading ACK(ack) ACK(ack+1)... after each one (not after the last
 * one unless arm_last); updates the checksum. Returns 0 on timeout. */
int  sio_recv(u32 *dst, u32 n, u32 ack, int arm_last, u32 *ck, u32 timeout_ms);
#endif
