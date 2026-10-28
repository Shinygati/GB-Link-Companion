/* Burn2Slot for GB-Link - link protocol shared by the GBA program and the web host.
 *
 * The GB-Link adapter is the SPI master (GB Link mode, 4 bytes per transfer), the GBA is the
 * 32-bit normal-mode slave. Every transfer swaps one 32-bit word each way: the host's word goes
 * to the GBA, and the host receives whatever word the GBA had loaded beforehand.
 *
 * Host -> GBA packet:  HEADER, args[nargs], (WRITE only: data[args[1]]), CHECKSUM
 *   - the GBA answers word k of a packet (k >= 1) with ACK(k); word 0 is answered with IDLE.
 *   - after the last word the GBA stops listening while it works.
 * The host then sends POLL words until it sees RESULT(status, count), followed by `count` data
 * words, after which the GBA loads IDLE again and waits for the next packet.
 */
#ifndef PROTO_H
#define PROTO_H

#define B2S_VERSION        0x00010100u            /* 1.1.0: the streamed transfers below */

#define W_POLL             0x504F4C4Cu            /* "POLL" */
#define W_IDLE             0x49444C45u            /* "IDLE" */
/* Controller mode: between packets, the host may send KEYS followed by POLL. The GBA answers the
 * POLL with KEYS_REPLY(buttons held: KEYINPUT bits, 1 = pressed), then loads IDLE again. */
#define W_KEYS             0x4B455953u            /* "KEYS" */
#define W_KEYS_REPLY(k)    (0x4B000000u | ((k) & 0x3FFu))
#define W_ACK(k)           (0xA5000000u | ((k) & 0xFFFFFFu))
#define W_HEADER(cmd, n)   (0x42000000u | ((u32)(cmd) << 16) | (n))
#define W_RESULT(st, cnt)  (0x52000000u | ((u32)(st) << 16) | (cnt))

#define IS_HEADER(w)       (((w) >> 24) == 0x42u)
#define HDR_CMD(w)         (((w) >> 16) & 0xFFu)
#define HDR_NARGS(w)       ((w) & 0xFFFFu)

/* commands */
#define CMD_HELLO          1   /* -> [version]                                              */
#define CMD_DETECT         2   /* -> [flashid, manufacturer, size, bufferSize, busType, seqType/intelType, kind] */
#define CMD_ERASE          3   /* args [wordAddr, totalWords] -> erase one 0x8000-word step */
#define CMD_WRITE          4   /* args [byteOffset, nwords, totalBytes] + data -> write + verify */
#define CMD_FINISH         5   /* args [ok] -> show the final message                         */
#define CMD_SAVE_INFO      6   /* -> [saveType, flashChipId, title0, title1, title2, gameCode] */
#define CMD_SAVE_READ      7   /* args [saveType, offset, nbytes<=16384] -> [checksum, data...] or [checksum, word] */
#define CMD_SAVE_WRITE     8   /* args [saveType, offset, nbytes<=4096] + data -> write + verify */
#define CMD_SHOW           9   /* args [message] -> show a status message (MSG_*)           */
#define CMD_ROM_INFO       10  /* -> [romBytes or 0xFFFFFFFF if no cart, header words 0..47 (0xC0 bytes)] */
#define ROM_SIZE_GUESSED   0x80000000u  /* in ROM_INFO's size: the size is a best guess (repro cart
                                            with other data after the game) - ask the player */
#define ROM_SIZE_REPRO     0x40000000u  /* in ROM_INFO's size: the cart didn't show where the game ends
                                            (a repro cart's flash chip); the size was measured */
#define ROM_SIZE_FLAGS     (ROM_SIZE_GUESSED | ROM_SIZE_REPRO)
#define CMD_ROM_READ       11  /* args [offset, nbytes<=MAX_ROM_CHUNK, step, steps] -> [checksum, data...] */
#define CMD_RTC_READ       13  /* -> [found, status, datetime bytes 0-3, bytes 4-6]: the cartridge's real-time clock
                                  (Seiko S-3511, as Pokemon Ruby/Sapphire/Emerald have), read the way the games
                                  read it. found 0: no answer. datetime: BCD year, month, day, weekday, hour,
                                  minute, second */
#define CMD_STATUS         12  /* args [op, nwords, ...] + data[nwords] -> draw a status screen (ST_OP_*) */
/* Streamed transfers, from GB-Link Cart Doctor's GBA cart dumper (gba_cart_dumper/main.c, MIT;
 * based on FIX94's GBA Link Cable Dumper). Data moves as a plain stream of words with no
 * acknowledgement per word, in 256-byte sections that each end with an XOR check:
 *   CMD_SAVE_DUMP   args [saveType, offset, nbytes, step, steps]: the GBA reads the save into its
 *                   RAM, repeats G_DONE until the host answers S_CONT, then sends the sections
 *                   (64 words + XOR; the host answers S_CONT, S_FAIL to have it again, or
 *                   S_ABORT). Then it's idle again: no RESULT.
 *   CMD_SAVE_PUT    args [saveType, offset, nbytes, step, steps]: the GBA repeats G_READY until
 *                   the host answers S_GO, receives the sections (64 words + XOR; then the host
 *                   sends S_CONT and gets G_OKOK, or G_FAIL to send it again), writes them to the
 *                   save chip and checks them, and answers with the normal RESULT.
 *   CMD_ROM_STREAM  args [nbytes]: G_DONE gate, then the ROM as records sent without waiting:
 *                   R_DATA|section + 64 words + check, R_FILL|section + value + count + check (a
 *                   run of sections that are one repeated word), R_END|sections. The host keeps
 *                   sending S_SPOLL; S_SEEK|section restarts from a section, S_FIN ends it.
 * nbytes is a multiple of 256 (flash saves: whole 4 KB sectors), at most 64 KB a command for saves.
 * An argument the GBA can't use gets the normal RESULT with an error instead. */
#define CMD_SAVE_DUMP      14
#define CMD_SAVE_PUT       15
#define CMD_ROM_STREAM     16
#define S_GO               0x4742600Du
#define S_CONT             0x4742C047u
#define S_FAIL             0x4742BAD0u
#define S_ABORT            0x4742AB0Bu
#define S_SPOLL            0x47425EEDu
#define S_FIN              0x4742F1A1u
#define S_SEEK             0x53000000u
#define G_READY            0x52454459u            /* "REDY" */
#define G_DONE             0x444F4E45u            /* "DONE" */
#define G_OKOK             0x4F4B4F4Bu            /* "OKOK" */
#define G_FAIL             0x4641494Cu            /* "FAIL" */
#define G_SYNC             0x53594E43u            /* "SYNC" */
#define R_DATA             0xDA000000u
#define R_FILL             0xF1000000u
#define R_END              0xE0000000u
#define STREAM_SECTION     0x100u
#define MAX_STREAM_SAVE    0x10000u              /* a 128 KB save is two of them */

#define MAX_SAVE_CHUNK     4096u
#define MAX_SAVE_READ_CHUNK 0x4000u   /* reads: 16 KB a command. A chunk that's all one word (blank)
                                       * comes back as [checksum, word]; so does a ROM chunk.     */
#define MAX_ROM_CHUNK      0x8000u

/* CMD_SHOW messages */
#define MSG_CONTROLLER     1   /* buttons are being sent to mGBA (arg 1: 1 = say how to open the status menu) */
#define MSG_BACKUP_STOPPED 2   /* a cartridge backup was cancelled or failed                  */
#define MSG_BACKUP_SAVE    3   /* the save about to be read is part of a backup               */
#define MSG_BACKUP_DONE    4   /* a cartridge backup finished                                 */
#define MSG_LANG           5   /* arg 1: the language of the text (E F D I S J) - the Pokemon game's */

/* CMD_STATUS ops: GB-Link Companion's status screens (gba/status.c). The PC builds the
 * screens (menus, Pokemon info, map) and the GBA draws them; any other command puts the normal
 * screen back first. */
#define ST_OP_CLEAR        1   /* blank status screen                                          */
#define ST_OP_ROWS         2   /* data: per row [row, 10 words of chars (4 each), 5 words of colours (8 each)] */
#define ST_OP_MAP          3   /* args [op, n, w, h, x0, y0], data: map cells, 4 per word (0 empty, 1 route, 2 town, 3 place, 4 here) */
#define ST_OP_MARK         4   /* args [op, 0, x, y, on, which]: the player's spot (0) or the map cursor (1) */
#define ST_OP_LEAVE        5   /* back to the normal screen                                    */
#define ST_OP_SPRITE       6   /* args [op, n, slot, x, y, w, h, flags], data: 16 colours (8 words), then 4bpp tiles;
                                  n = 0 removes the slot's picture. Colour 0 shows the screen's
                                  background, or with flags bit 0 the kept background picture. */
#define ST_SPRITES         8
#define ST_OP_IMAGE8       8   /* args [op, n, x0, y0, w, h, ncolours], data: colours (2 a word), then a byte a pixel.
                                  Drawn 1:1 and kept, as the background under pictures (ST_OP_SPRITE flag 1) */
#define ST_OP_RESTORE      9   /* args [op, 0, x0, y0, w, h]: draw that part of the kept background again */
#define ST_OP_IMAGE        7   /* args [op, n, w, h, x0, y0, boxW, boxH], data: w*h colours (2 a word),
                                  scaled to fit the box (same shape), centred                  */
#define ST_ROW_WORDS       16
#define ST_MAP_CELL        8   /* map cells are 8x8 pixels                                     */
#define ST_MAP_MAX         (28 * 15)

#define MAX_ARGS           8
#define MAX_BLOCK_WORDS    0x4000u                /* 64 KB per WRITE packet */

/* status codes */
#define ST_OK              0
#define ST_UNKNOWN_FLASH   1
#define ST_TIMEOUT         2
#define ST_CHECKSUM        3
#define ST_WRITE_FAIL      4   /* data: [byteAddr]                 */
#define ST_VERIFY_FAIL     5   /* data: [byteAddr, got, expected]  */
#define ST_BAD_COMMAND     6
#define ST_TOO_BIG         7
#define ST_NO_SAVE         8

/* chip kinds (which Burn2Slot erase/write routines are used) */
#define KIND_UNKNOWN       0
#define KIND_INTEL         1
#define KIND_22XX          2

/* checksum over every word of a packet before the trailer */
#define CK_INIT            0x12345678u
#define CK_STEP(c, w)      ((((c) << 5) | ((c) >> 27)) + (w))

#endif
