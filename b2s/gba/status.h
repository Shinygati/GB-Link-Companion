#ifndef STATUS_H
#define STATUS_H
#include "gba.h"
/* Status screens for GB-Link Companion (CMD_STATUS): the PC draws a 40x17 text grid
 * (5x8 font in 6x9 cells) and an optional region map, and the GBA just shows them. The PC runs
 * the menus from the buttons it reads with KEYS; the GBA only draws what it's sent. */
#define ST_COLS 40
#define ST_ROWS 17

/* run one CMD_STATUS packet (args[0] = op, see proto.h); returns a status code */
u32  status_command(const u32 *args, u32 nargs, const u32 *data, u32 ndata);
/* is a status screen up (then any other command puts the normal screen back first) */
int  status_active(void);
/* leave the status screen: blank it; the caller redraws its own screen */
void status_leave(void);
#endif
