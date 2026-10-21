/* Burn2Slot for GB-Link: GBA multiboot side. Receives commands and ROM data from the web page
 * over the link cable and flashes the bootleg cart with the Burn2Slot routines (flash.c). */
#include "gba.h"
#include "proto.h"
#include "flash.h"
#include "sio.h"
#include "ui.h"
#include "save.h"
#include "status.h"

#define T_PACKET_MS  250    /* gap allowed between words inside a packet  */
#define T_RESULT_MS  3000   /* how long to wait for the host to collect a result */

/* reading the cart ROM (the simulator has its own cart) */
#ifdef B2S_SIM
u16 sim_cart_rd(u32 a);
#define ROM16(i)  sim_cart_rd(i)
#define ROM32(i)  ((u32)sim_cart_rd(2 * (i)) | (u32)sim_cart_rd(2 * (i) + 1) << 16)
#else
#define ROM16(i)  (((vu16 *)0x08000000)[i])
#define ROM32(i)  (((vu32 *)0x08000000)[i])
#endif

static u32 args[MAX_ARGS];
static int backup_save;   /* the save being read is part of a cartridge backup (MSG_BACKUP_SAVE) */
static u32 block[MAX_BLOCK_WORDS];
static u32 res[8];
static const u32 *res_ptr;

/* hand a result to the host: RESULT header, n data words, then IDLE.
 * Runs from IWRAM: the GBA has to be listening again within the gap between two words. */
IWRAM_CODE static void send_result(u32 status, const u32 *data, u32 n)
{
    for (int attempt = 0; attempt < 100; attempt++) {
        sio_resync_arm(W_RESULT(status, n));
        u32 i = 0, w;
        for (;;) {
            if (!sio_wait(&w, T_RESULT_MS)) { sio_resync_arm(W_IDLE); return; }   /* host gone */
            if (w != W_POLL) break;                     /* out of step: start the result again */
            if (i < n) sio_arm(data[i++]);
            else { sio_arm(W_IDLE); return; }
        }
    }
    sio_resync_arm(W_IDLE);
}

/* Wait for a complete packet (answering POLLs with IDLE meanwhile). Returns 1 if its checksum
 * is good, 0 if not. Runs from IWRAM so every word is acknowledged within the gap. */
IWRAM_CODE static int receive_packet(u32 *cmd_out, u32 *nargs_out, u32 *ndata_out)
{
    for (;;) {
        u32 w;
        if (!sio_wait(&w, 0)) continue;
        if (w == W_POLL) { sio_arm(W_IDLE); continue; }
        if (w == W_KEYS) { sio_arm(W_KEYS_REPLY(~REG_KEYINPUT)); continue; }   /* controller mode */
        if (!IS_HEADER(w) || HDR_NARGS(w) > MAX_ARGS) { sio_resync_arm(W_IDLE); continue; }

        u32 cmd = HDR_CMD(w), nargs = HDR_NARGS(w), ndata = 0, trailer, ck = CK_STEP(CK_INIT, w);
        sio_arm(W_ACK(1));
        if (nargs && !sio_recv(args, nargs, 2, 1, &ck, T_PACKET_MS)) { sio_resync_arm(W_IDLE); continue; }
        if (cmd == CMD_WRITE || cmd == CMD_SAVE_WRITE || (cmd == CMD_STATUS && nargs >= 2 && args[1])) {
            ndata = cmd == CMD_WRITE ? (nargs >= 2 ? args[1] : 0) : cmd == CMD_STATUS ? args[1]
                  : (nargs >= 3 && args[2] <= MAX_SAVE_CHUNK ? args[2] / 4 : 0);
            if (ndata == 0 || ndata > MAX_BLOCK_WORDS) { sio_resync_arm(W_IDLE); continue; }
            if (!sio_recv(block, ndata, nargs + 2, 1, &ck, T_PACKET_MS)) { sio_resync_arm(W_IDLE); continue; }
        }
        u32 ck_payload = ck;
        if (!sio_recv(&trailer, 1, 0, 0, &ck, T_PACKET_MS)) { sio_resync_arm(W_IDLE); continue; }
        *cmd_out = cmd; *nargs_out = nargs; *ndata_out = ndata;
        return trailer == ck_payload;
    }
}

static void show_chip(void)
{
    ui_clear_line(3); ui_clear_line(4); ui_clear_line(5);
    if (flash_kind() == KIND_UNKNOWN) {
        ui_text(3, 1, C_RED, "Unknown flash  ID:");
        ui_hex(3, 20, C_RED, cart.flashid, 4);
        ui_text(4, 1, C_GREY, "Not a supported repro cart?");
        return;
    }
    ui_text(3, 1, C_WHITE, "Flash ID:");
    ui_hex(3, 11, C_CYAN, cart.flashid, 4);
    ui_text(3, 17, C_GREY, flash_kind() == KIND_INTEL ? "(Intel)" : "(22XX)");
    ui_text(4, 1, C_WHITE, "Maker:");
    ui_text(4, 8, C_CYAN, flash_manufacturer((u8)cart.manufactorID));
    ui_text(5, 1, C_WHITE, "Size:");
    if (cart.size) { ui_dec(5, 7, C_CYAN, cart.size >> 20); ui_text(5, 7 + (cart.size >= (10u << 20) ? 2 : 1), C_CYAN, " MB"); }
    else ui_text(5, 7, C_GREY, "unknown (no CFI)");
}

static void status_line(int color, const char *s)
{
    ui_clear_line(8);
    ui_text(8, 1, color, s);
}

/* Text that differs between the two builds: the web page's flasher (Burn2Slot for GB-Link) and
 * the helper the mGBA save-sync launcher sends (built with -DSYNC_UI). */
#ifdef SYNC_UI
#define T_TITLE      "GB-Link Companion for mGBA"
#define T_SUBTITLE   "Playing in mGBA on your PC"
#define T_FOOT1      "Keep the link cable in."
#define T_FOOT2      "Don't switch off mid-save."
#define T_WAITING    "Waiting for the PC..."
#define T_HELLO      "Connected to the PC"
#define T_INFO_DONE  "Cartridge found"
#define T_READING    "Loading save into mGBA..."
#define T_READ_DONE  "Save loaded into mGBA"
#define T_WRITING    "Saving to the cart..."
#define T_WRITE_DONE "Save written to the cart"
#define L_READ       "Load"
#define T_ROM_READ   "Copying the game to the PC..."
#define T_ROM_DONE   "Game copied to the PC"
#define L_WRITE      "Save"
#else
#define T_TITLE      "Burn2Slot for GB-Link"
#define T_SUBTITLE   "Repro cart flasher v1.0"
#define T_FOOT1      "Keep the link cable connected"
#define T_FOOT2      "and follow the web page."
#define T_WAITING    "Waiting for the web page..."
#define T_HELLO      "Connected to the web page"
#define T_INFO_DONE  "Ready to back up / restore"
#define T_READING    "Backing up the save..."
#define T_READ_DONE  "Save backed up"
#define T_WRITING    "Restoring the save..."
#define T_WRITE_DONE "Save restored"
#define L_READ       "Read"
#define T_ROM_READ   "Dumping the ROM..."
#define T_ROM_DONE   "ROM dumped"
#define L_WRITE      "Write"
#endif

/* Progress on row 7. The bar shows work that is finished: it's drawn before an operation with
 * what was done so far, and again afterwards. A new kind of operation clears the row first. */
static u32 cur_op;
static void progress(u32 op, const char *label, const char *status, u32 done, u32 total)
{
    if (op != cur_op) {
        cur_op = op;
        status_line(C_YELLOW, status);
        ui_clear_line(7);
        ui_text(7, 1, C_WHITE, label);
    }
    ui_bar(7, done, total);
}

/* save progress: in steps when the host says which step this is (args 3 and 4: the launcher only
 * writes the 4 KB blocks that changed), otherwise by position in the save. */
static void save_progress(u32 op, const char *label, const char *status, u32 nargs, int after)
{
    if (nargs >= 5 && args[4]) progress(op, label, status, args[3] + (after ? 1 : 0), args[4]);
    else progress(op, label, status, args[1] + (after ? args[2] : 0), save_size(args[0]));
}

#ifndef SYNC_UI
static const char *const save_names[] = {"none", "SRAM 32 KB", "SRAM 64 KB", "Flash 64 KB", "Flash 128 KB", "EEPROM 512 B", "EEPROM 8 KB"};
#endif

/* the normal screen: title, subtitle and the footer */
static void draw_home(void)
{
    ui_init();
    ui_text(0, 1, C_WHITE, T_TITLE);
    ui_text(1, 1, C_GREY, T_SUBTITLE);
    ui_text(10, 1, C_GREY, T_FOOT1);
    ui_text(11, 1, C_GREY, T_FOOT2);
    cur_op = 0;
}

static u32 run_command(u32 cmd, u32 nargs, u32 ndata, u32 *nres)
{
    *nres = 0;
    if (status_active() && cmd != CMD_STATUS) {   /* e.g. a save to write: show the normal screen */
        status_leave();
        draw_home();
    }
    res_ptr = res;
    if (cmd != CMD_ERASE && cmd != CMD_WRITE && cmd != CMD_SAVE_READ && cmd != CMD_SAVE_WRITE && cmd != CMD_ROM_READ)
        cur_op = 0;
    switch (cmd) {
    case CMD_SAVE_INFO: {
        status_line(C_YELLOW, "Checking the cartridge...");
        u32 fid, t = save_detect(&fid);
        res[0] = t; res[1] = fid; save_title(&res[2]); *nres = 6;
        ui_clear_line(3); ui_clear_line(4); ui_clear_line(5);
        char title[13]; for (int i = 0; i < 12; i++) { char c = (char)(res[2 + i / 4] >> (8 * (i & 3))); title[i] = (c >= 32 && c < 127) ? c : ' '; } title[12] = 0;
        ui_text(3, 1, C_WHITE, "Game:"); ui_text(3, 7, C_CYAN, title);
#ifndef SYNC_UI
        ui_text(4, 1, C_WHITE, "Save:"); ui_text(4, 7, C_CYAN, save_names[t]);
#endif
        ui_clear_line(7);
        status_line(C_GREEN, T_INFO_DONE);
        return ST_OK;
    }
    case CMD_SAVE_READ: {
        if (nargs < 3 || args[2] > MAX_SAVE_READ_CHUNK || (args[2] & 3)) return ST_BAD_COMMAND;
        if (!save_size(args[0])) return ST_NO_SAVE;
        const char *reading = backup_save ? "Copying the save to the PC..." : T_READING;
        save_progress(CMD_SAVE_READ, L_READ, reading, nargs, 0);
        u8 *buf = (u8 *)(block + 1);
        if (save_read(args[0], args[1], args[2], buf)) return ST_TOO_BIG;
        save_progress(CMD_SAVE_READ, L_READ, reading, nargs, 1);
        u32 ck = CK_INIT, n = args[2] / 4, same = 1;
        for (u32 i = 0; i < n; i++) { ck = CK_STEP(ck, block[1 + i]); same &= block[1 + i] == block[1]; }
        block[0] = ck;
        res_ptr = block; *nres = same && n > 1 ? 2 : n + 1;   /* all one word (blank): just that word */
        if (nargs >= 5 ? args[3] + 1 >= args[4] : args[1] + args[2] >= save_size(args[0])) status_line(C_GREEN, backup_save ? "Save copied to the PC" : T_READ_DONE);
        return ST_OK;
    }
    case CMD_SAVE_WRITE: {
        if (nargs < 3 || !save_size(args[0])) return ST_NO_SAVE;
        save_progress(CMD_SAVE_WRITE, L_WRITE, T_WRITING, nargs, 0);
        u8 got = 0;
        int bad = save_write(args[0], args[1], args[2], (const u8 *)block, &got);
        if (bad == -2) return ST_TOO_BIG;
        if (bad >= 0) { res[0] = (u32)bad; res[1] = got; res[2] = ((const u8 *)block)[bad - (int)args[1]]; *nres = 3; return ST_VERIFY_FAIL; }
        save_progress(CMD_SAVE_WRITE, L_WRITE, T_WRITING, nargs, 1);
        if (nargs >= 5 ? args[3] + 1 >= args[4] : args[1] + args[2] >= save_size(args[0])) status_line(C_GREEN, T_WRITE_DONE);
        return ST_OK;
    }
    case CMD_HELLO:
        res[0] = B2S_VERSION; *nres = 1;
        backup_save = 0;
        status_line(C_GREEN, T_HELLO);
        return ST_OK;

    case CMD_DETECT: {
        status_line(C_YELLOW, "Detecting flash chip...");
        int k = flash_detect();
        res[0] = cart.flashid; res[1] = cart.manufactorID; res[2] = cart.size;
        res[3] = (u32)cart.bufferSize | (u32)cart.busType << 16 | (u32)cart.adressSeqType << 24;
        *nres = 4;
        show_chip();
        status_line(k ? C_GREEN : C_RED, k ? "Ready - choose a ROM" : "No supported flash found");
        return k ? ST_OK : ST_UNKNOWN_FLASH;
    }

    case CMD_ERASE:
        if (nargs < 2) return ST_BAD_COMMAND;
        if (flash_kind() == KIND_UNKNOWN) return ST_UNKNOWN_FLASH;
        if (args[0] >= 0x01000000u) return ST_TOO_BIG;
        progress(CMD_ERASE, "Erase", "Erasing...", args[0], args[1]);
        if (flash_erase_step(args[0])) return ST_TIMEOUT;
        progress(CMD_ERASE, "Erase", "Erasing...", args[0] + 0x8000, args[1]);
        return ST_OK;

    case CMD_WRITE: {
        if (nargs < 3) return ST_BAD_COMMAND;
        if (flash_kind() == KIND_UNKNOWN) return ST_UNKNOWN_FLASH;
        u32 off = args[0], words16 = ndata * 2;
        if ((off & 1) || off + ndata * 4 > 0x02000000u) return ST_TOO_BIG;
        progress(CMD_WRITE, "Write", "Writing...", off, args[2]);
        const u16 *w = (const u16 *)block;
        int bad = flash_write_block(off, w, words16);
        if (bad >= 0) { res[0] = off + (u32)bad * 2; *nres = 1; return ST_WRITE_FAIL; }
        u16 got;
        bad = flash_verify_block(off, w, words16, &got);
        if (bad >= 0) { res[0] = off + (u32)bad * 2; res[1] = got; res[2] = w[bad]; *nres = 3; return ST_VERIFY_FAIL; }
        progress(CMD_WRITE, "Write", "Writing...", off + ndata * 4, args[2]);
        return ST_OK;
    }

    case CMD_ROM_INFO: {
        /* the size: the first power-of-two boundary (from 1 MB) where the ROM has ended. Past the
         * end, either
         *  - the cart bus answers each halfword with its own address (open bus: 0, 1, 2, ...;
         *    the method GB-Link Cart Doctor uses), or
         *  - the ROM chip shows up again from its start (a mirror: many official carts don't
         *    wire the top address lines, e.g. a 16 MB Pokemon cart repeats at 16 MB).
         * 8 KB is compared, header included, so a real game can't match by chance. */
        status_line(C_YELLOW, "Checking the cartridge...");
        u32 size = 0xFFFFFFFFu;
        if (ROM32(1) == 0x51AEFF24u) {                          /* the logo is there: a cart */
            for (size = 1u << 20; size < (1u << 25); size <<= 1) {
                int open = 1, mirror = 1;
                for (u32 j = 0; j < 0x1000 && (open || mirror); j++) {
                    u16 v = ROM16(size / 2 + j);
                    if (v != (u16)j) open = 0;
                    if (v != ROM16(j)) mirror = 0;
                }
                if (open || mirror) break;
            }
            /* No end found on the bus: a repro cart's flash chip (usually 32 MB) with the game in
             * its start and erased flash (FF) after it. Measure where the game's data ends and
             * round up to a power of two (Pokemon Ruby: data to 14.7 MB -> 16 MB). Official carts
             * never get here (their end shows on the bus), so their size is never trimmed. */
            if (size == 1u << 25) {
                status_line(C_YELLOW, "Measuring the game...");
                u32 i = (1u << 25) / 4;
                while (i > 0 && ROM32(i - 1) == 0xFFFFFFFFu) i--;
                u32 end = i * 4, p2 = 1u << 20;
                while (p2 < end) p2 <<= 1;
                size = p2;
                /* Still 32 MB: something is stored after the game (an older, bigger game left in
                 * the flash). Games are padded with FF up to their size, so guess the biggest
                 * size that ends in at least 4 KB of FF, and let the PC ask (ROM_SIZE_GUESSED). */
                if (size == 1u << 25) {
                    for (u32 p = 1u << 24; p >= 1u << 20; p >>= 1) {
                        int pad = 1;
                        for (u32 k = (p - 4096) / 4; k < p / 4; k++)
                            if (ROM32(k) != 0xFFFFFFFFu) { pad = 0; break; }
                        if (pad) { size = p | ROM_SIZE_GUESSED; break; }
                    }
                }
                size |= ROM_SIZE_REPRO;
            }
        }
        block[0] = size;
        for (u32 i = 0; i < 0xC0 / 4; i++) block[1 + i] = ROM32(i);
        res_ptr = block; *nres = 1 + 0xC0 / 4;
        if (size == 0xFFFFFFFFu) { status_line(C_RED, "No cartridge found"); return ST_NO_SAVE; }
        u32 shown = size & ~ROM_SIZE_FLAGS;
        char title[13]; for (int i = 0; i < 12; i++) { char c = (char)(block[1 + 0xA0 / 4 + i / 4] >> (8 * (i & 3))); title[i] = (c >= 32 && c < 127) ? c : ' '; } title[12] = 0;
        ui_clear_line(3); ui_clear_line(4); ui_clear_line(5); ui_clear_line(7);
#ifdef SYNC_UI
        ui_clear_line(1); ui_text(1, 1, C_GREY, "Backing up a cartridge");
#endif
        ui_text(3, 1, C_WHITE, "Game:"); ui_text(3, 7, C_CYAN, title);
        ui_text(4, 1, C_WHITE, "Size:"); ui_dec(4, 7, C_CYAN, shown >> 20); ui_text(4, 7 + (shown >= (10u << 20) ? 2 : 1), C_CYAN, (size & ROM_SIZE_GUESSED) ? " MB?" : " MB");
        status_line(C_GREEN, T_INFO_DONE);
        return ST_OK;
    }
    case CMD_ROM_READ: {
        if (nargs < 2 || args[1] == 0 || args[1] > MAX_ROM_CHUNK || (args[1] & 3) || args[0] + args[1] > (1u << 25))
            return ST_BAD_COMMAND;
        u32 done = nargs >= 4 ? args[2] : 0, steps = nargs >= 4 ? args[3] : 0;
        if (steps) progress(CMD_ROM_READ, "Copy", T_ROM_READ, done, steps);
        u32 ck = CK_INIT, n = args[1] / 4, first = args[0] / 4;
        u32 same = 1;
        for (u32 i = 0; i < n; i++) { u32 w = ROM32(first + i); block[1 + i] = w; ck = CK_STEP(ck, w); same &= w == block[1]; }
        block[0] = ck;
        res_ptr = block; *nres = same && n > 1 ? 2 : n + 1;   /* all one word (padding): just that word */
        if (steps) {
            progress(CMD_ROM_READ, "Copy", T_ROM_READ, done + 1, steps);
            if (done + 1 >= steps) status_line(C_GREEN, T_ROM_DONE);
        }
        return ST_OK;
    }

    case CMD_SHOW:
        if (nargs < 1) return ST_BAD_COMMAND;
        switch (args[0]) {
        case MSG_CONTROLLER:
            backup_save = 0;
            ui_clear_line(1); ui_text(1, 1, C_GREY, T_SUBTITLE);
            ui_clear_line(7);
#ifdef SYNC_UI
            if (nargs >= 2 && args[1]) ui_text(7, 1, C_GREY, "Hold SELECT for the menu");
#endif
            status_line(C_CYAN, "GBA buttons control mGBA");
            break;
        case MSG_BACKUP_SAVE:
            backup_save = 1;
            break;
        case MSG_BACKUP_STOPPED:
            backup_save = 0;
            ui_clear_line(7);
            ui_text(7, 1, C_GREY, "Nothing was saved.");
            status_line(C_YELLOW, "Backup stopped");
            break;
        case MSG_BACKUP_DONE:
            backup_save = 0;
            ui_clear_line(7);
            ui_text(7, 1, C_GREY, "You can switch the GBA off.");
            status_line(C_GREEN, "Backup finished");
            break;
        }
        return ST_OK;

    case CMD_STATUS:
        if (nargs >= 1 && args[0] == ST_OP_LEAVE) {
            if (status_active()) status_leave();
            draw_home();                       /* mGBA was closed: the helper's own screen */
            ui_clear_line(7);
            status_line(C_GREEN, "mGBA closed");
            return ST_OK;
        }
        return status_command(args, nargs, block, ndata);

    case CMD_FINISH:
        if (nargs >= 1 && args[0]) {
            status_line(C_GREEN, "Done! Switch off, then play.");
            ui_text(7, 1, C_WHITE, "Write");
            ui_bar(7, 1, 1);
        } else {
            status_line(C_RED, "Stopped - see the web page");
        }
        return ST_OK;
    }
    return ST_BAD_COMMAND;
}

int main(void)
{
    draw_home();
    status_line(C_YELLOW, T_WAITING);
    sio_init();
    sio_resync_arm(W_IDLE);

    for (;;) {
        u32 cmd, nargs, ndata, n;
        int ok = receive_packet(&cmd, &nargs, &ndata);
        u32 st = ok ? run_command(cmd, nargs, ndata, &n) : (n = 0, ST_CHECKSUM);
        send_result(st, res_ptr, n);
    }
}
