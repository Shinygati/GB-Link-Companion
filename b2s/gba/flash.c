/* Burn2Slot flashing routines, ported from the Nintendo DS version (vrodin/Burn2Slot, MIT License,
 * arm9/source/app.cpp) to run on the Game Boy Advance itself.
 *
 * The DS reaches the GBA cart through Slot-2 at 0x08000000; a GBA sees its own cart at the same
 * address, so detection, the bus/address-sequence variants, CFI size query, erase and word
 * programming are the same code. Changes from the DS version:
 *   - the ROM data arrives over the link cable (one 64 KB block at a time) instead of from SD,
 *   - every busy-wait has a timeout so a bad cart reports an error instead of hanging,
 *   - a failed word is retried once instead of erasing its sector again (which would wipe the
 *     words already written in that sector), and every block is read back and verified,
 *   - erased (0xFFFF) words are skipped when programming, since NOR flash already holds 0xFFFF,
 *   - the 22XX command words for the detected bus type are precomputed and the hot loops run
 *     from IWRAM, so programming isn't slowed down by the GBA's slower CPU.
 */
#include "gba.h"
#include "flash.h"
#include "proto.h"

#ifndef B2S_SIM
#define cart_rd(a)      (CART16[(a)])
#define cart_wr(a, v)   (CART16[(a)] = (u16)(v))
#else
u16  sim_cart_rd(u32 a);
void sim_cart_wr(u32 a, u16 v);
#define cart_rd(a)      sim_cart_rd(a)
#define cart_wr(a, v)   sim_cart_wr((a), (u16)(v))
#endif

#define WRITE_BUS_COUNT 6
#define ADD_SEQ_COUNT   6

CartInfo cart;

static const u32 seq_0[] = {0x555,  0x2AA,  0x555,  0x555,  0x2AA,  0x555};
static const u32 seq_1[] = {0xAAA,  0x555,  0xAAA,  0xAAA,  0x555,  0xAAA};
static const u32 seq_2[] = {0xAAA,  0x554,  0xAAA,  0xAAA,  0x554,  0xAAA};
static const u32 seq_3[] = {0x5555, 0x2AAA, 0x5555, 0x5555, 0x2AAA, 0x5555};
static const u32 seq_4[] = {0xAAAA, 0x5554, 0xAAAA, 0xAAAA, 0x5554, 0xAAAA};
static const u32 seq_5[] = {0x1554, 0xAAA,  0x1554, 0x1554, 0xAAA,  0x1554};
static const u32 *const seqs[ADD_SEQ_COUNT] = {seq_0, seq_1, seq_2, seq_3, seq_4, seq_5};

static const u8 bus_1[] = {15, 7, 14, 6, 13, 5, 12, 4, 0, 8, 1, 9, 2, 10, 3, 11};
static const u8 bus_2[] = {0, 8, 1, 9, 6, 14, 7, 15, 2, 10, 3, 11, 4, 12, 5, 13};

static const u32 lag = 0xF;

/* ---------------------------------------------------------------- helpers (as in Burn2Slot) */
static inline __attribute__((always_inline)) void wait(u32 count)
{
#ifndef B2S_SIM
    while (count) { __asm__ volatile("nop"); count--; }
#else
    (void)count;
#endif
}

static u16 swapBits(u16 data)
{
    u16 bit1 = (data >> 0) & 1, bit2 = (data >> 1) & 1;
    u16 x = bit1 ^ bit2;
    x = (x << 0) | (x << 1);
    return data ^ x;
}

static u16 swapTo(const u8 *bus, u16 data)
{
    u16 result = 0;
    for (int i = 0; i < 16; ++i) result |= ((data >> i) & 1) << bus[i];
    return result;
}

static u16 swapFrom(const u8 *bus, u16 data)
{
    u16 result = 0;
    for (int i = 0; i < 16; ++i) result |= ((data >> bus[i]) & 1) << i;
    return result;
}

/* value actually put on the bus for a command byte, for the current bus type (write_word()) */
static u16 bus_out(u8 busType, u16 v)
{
    switch (busType) {
        case 1:  return swapBits(v);
        case 2:  return swapTo(bus_1, v);
        case 3:  return swapTo(bus_2, v);
        case 4:  return (u16)(v << 8 | v);
        case 5:  return swapBits((u16)(v << 8 | v));
        default: return v;
    }
}

/* value read back through the current bus type (read_word()) */
static u16 bus_in(u8 busType, u16 v)
{
    switch (busType) {
        case 1: case 5: return swapBits(v);
        case 2:         return swapFrom(bus_1, v);
        case 3:         return swapFrom(bus_2, v);
        default:        return v;
    }
}

static void write_word_rom(u32 address, u16 v) { cart_wr(address, v); wait(lag); }
static u16  read_word_rom(u32 address)         { return cart_rd(address); }
static void write_word(u32 address, u16 v)     { write_word_rom(address, bus_out(cart.busType, v)); }
static u16  read_word(u32 address)             { return bus_in(cart.busType, read_word_rom(address)); }

/* ------------------------------------------------------------------------ 22XX (AMD-style) */
static const u32 *getAddressSeq22XX(void) { return seqs[cart.adressSeqType % ADD_SEQ_COUNT]; }

static void reset22XX(void)
{
    static const u8 data_sec[] = {0xAA, 0x55, 0xF0};
    const u32 *addr_sec = getAddressSeq22XX();
    for (u8 i = 0; i < 3; ++i) write_word(addr_sec[i], data_sec[i]);
}

static void detect22XX(void)
{
    static const u8 data_sec[] = {0xAA, 0x55, 0x90};
    u16 flashid, manufactorID;
    for (cart.adressSeqType = 0; cart.adressSeqType < ADD_SEQ_COUNT; cart.adressSeqType++) {
        const u32 *addr_sec = getAddressSeq22XX();
        for (cart.busType = 0; cart.busType < WRITE_BUS_COUNT; cart.busType++) {
            for (u8 i = 0; i < 3; ++i) write_word(addr_sec[i], data_sec[i]);
            flashid = read_word(0x1);
            manufactorID = read_word(0x0);
            if (((flashid >> 8) & 0xFF) == 0x22 || flashid == 0x7E7E || flashid == 0x57 || flashid == 0xF8) {
                cart.flashid = flashid;
                cart.manufactorID = manufactorID;
                reset22XX();
                return;
            }
        }
    }
    cart.adressSeqType = 0;
    cart.busType = 0;
}

static int eraseSector22XX(u32 addr)
{
    static const u8 data_sec[] = {0xAA, 0x55, 0x80, 0xAA, 0x55};
    const u32 *addr_sec = getAddressSeq22XX();
    for (u8 i = 0; i < 5; ++i) write_word(addr_sec[i], data_sec[i]);
    write_word(addr, 0x30);

    /* DQ7 goes high when the erase is done; ~25 s limit (the DS version waits forever) */
    u32 timeout = 0x2000000;
    u16 statusReg = 0;
    while ((statusReg | 0xFF7F) != 0xFFFF && timeout) {
        statusReg = read_word(addr);
        wait(1);
        timeout--;
    }
    reset22XX();
    return timeout ? 0 : -1;
}

/* precomputed "AA 55 A0" program-command addresses and bus values for the detected chip */
static u32 p_addr[3];
static u16 p_val[3];

static void prepare22XX(void)
{
    static const u8 data_sec[] = {0xAA, 0x55, 0xA0};
    const u32 *addr_sec = getAddressSeq22XX();
    for (int i = 0; i < 3; i++) { p_addr[i] = addr_sec[i]; p_val[i] = bus_out(cart.busType, data_sec[i]); }
}

IWRAM_CODE static int program22XX(u32 addr, u16 data)
{
    for (int i = 0; i < 3; i++) { cart_wr(p_addr[i], p_val[i]); wait(lag); }
    cart_wr(addr, data); wait(lag);
    int timeout = 0x2000;
    while (timeout && cart_rd(addr) != data) { timeout--; wait(1); }
    return timeout ? 0 : -1;
}

static int writeWord22XX(u32 addr, u16 data)
{
    if (program22XX(addr, data) == 0) return 0;
    reset22XX();                       /* DS version erased the sector here; retry instead */
    return program22XX(addr, data);
}

/* -------------------------------------------------------------------------------- Intel */
static int waitStatusIntel(u32 addr)
{
    u32 timeout = 0x2000000;
    u16 statusReg = 0;
    while ((statusReg | 0xFF7F) != 0xFFFF && timeout) {
        statusReg = read_word_rom(addr);
        wait(1);
        timeout--;
    }
    return timeout ? 0 : -1;
}

static int eraseSectorIntel(u32 addr)
{
    write_word_rom(addr, 0x50);
    write_word_rom(addr, 0x60);
    write_word_rom(addr, 0xD0);
    write_word_rom(addr, 0x20);
    write_word_rom(addr, 0xD0);

    int timeout = 0x10000;     /* waitForFlash(addr, WAIT_NON_FFFF, 0x10000) */
    while (timeout && read_word_rom(addr) == 0xFFFF) { wait(1); timeout--; }

    int r = waitStatusIntel(addr);
    write_word_rom(addr, 0xFF);
    return r;
}

IWRAM_CODE static int programIntel(u32 addr, u16 data)
{
    cart_wr(addr, 0x50); wait(lag);
    cart_wr(addr, 0xFF); wait(lag);
    cart_wr(addr, 0x40); wait(lag);
    cart_wr(addr, data); wait(lag);
    int timeout = 0x1000;
    while (timeout && cart_rd(addr) == 0xFFFF) { timeout--; wait(1); }
    while (timeout && cart_rd(addr) == 0)      { timeout--; wait(1); }
    while (timeout && !(cart_rd(addr) & 0x80)) { timeout--; wait(1); }
    return timeout ? (int)cart_rd(addr) : -1;   /* status register, or -1 on timeout */
}

static int writeWordIntel(u32 addr, u16 data)
{
    int st = programIntel(addr, data);
    if (st >= 0 && (st & 3)) {
        /* block was locked: unlock & erase, then try again (once) - as the DS version does */
        eraseSectorIntel(addr);
        st = programIntel(addr, data);
    }
    write_word_rom(addr, 0xFF);
    if (st < 0 || (st & 0x10)) return -1;
    return 0;
}

static void detectIntel(void)
{
    static const u16 bufSigns[] = {0x8902, 0x8904, 0x887D, 0x887E, 0x88B0};
    cart.intelType = 0;
    write_word_rom(0x0, 0xFF);
    write_word_rom(0x0, 0x50);
    write_word_rom(0x0, 0x90);
    u16 manufactorID = read_word_rom(0x0);
    u16 flashid = read_word_rom(0x1);

    for (u8 i = 0; i < 4; i++) {        /* (sic) the DS version checks only the first 4 */
        if (flashid == bufSigns[i]) {
            cart.manufactorID = manufactorID; cart.flashid = flashid; cart.intelType = 2;
            write_word_rom(0, 0xF0);
            return;
        }
    }
    if (((flashid >> 8) & 0xFF) == 0x88) {
        cart.manufactorID = manufactorID; cart.flashid = flashid; cart.intelType = 1;
        write_word_rom(0, 0xF0);
        return;
    }
    if (flashid == 0xB0 && manufactorID == 0xB0) {
        cart.manufactorID = manufactorID; cart.flashid = 0x88FF; cart.intelType = 1;
        write_word_rom(0, 0xF0);
        return;
    }
    if (manufactorID == 0x1C) {
        cart.manufactorID = manufactorID; cart.flashid = flashid; cart.intelType = 1;
        write_word_rom(0, 0xF0);
        return;
    }
    cart.intelType = 0;
}

static void chipSize(void)
{
    write_word(0x0, 0x50);
    write_word(0x0, 0xFF);
    write_word(0x55, 0x98);
    if (read_word(0x10) == 0x51 && read_word(0x11) == 0x52 && read_word(0x12) == 0x59) {
        u16 n = read_word(0x27), b = read_word(0x2A);
        cart.size = n < 32 ? (u32)1 << n : 0;
        cart.bufferSize = b < 16 ? (u16)(1 << b) : 0;
        write_word(0x0, 0x50);
        write_word(0x0, 0xFF);
        write_word(0, 0xF0);
    }
}

/* ------------------------------------------------------------------------------ public */
int flash_kind(void)
{
    u16 id = cart.flashid;
    if (!id) return KIND_UNKNOWN;
    if (((id >> 8) & 0xFF) == 0x88 || ((id >> 8) & 0xFF) == 0x89 || cart.manufactorID == 0x1C) return KIND_INTEL;
    if (((id >> 8) & 0xFF) == 0x22 || id == 0x7E7E || id == 0x57 || id == 0xF8) return KIND_22XX;
    return KIND_UNKNOWN;
}

int flash_detect(void)                   /* idFlashrom_GBA() + chipSize() */
{
    cart.busType = 0; cart.adressSeqType = 0;
    cart.flashid = 0; cart.manufactorID = 0; cart.size = 0; cart.bufferSize = 0;

    detectIntel();
    if (cart.flashid == 0) {
        detect22XX();
        if (cart.flashid != 0) reset22XX();
    }
    if (cart.flashid != 0) chipSize();
    if (flash_kind() == KIND_22XX) prepare22XX();
    return flash_kind();
}

/* one step of Burn2Slot's erase(): 0x8000 words (64 KB) starting at word address addr */
int flash_erase_step(u32 addr)
{
    if (flash_kind() == KIND_INTEL) {
        for (u32 smallAddr = addr; smallAddr < addr + 0x8000; smallAddr += 0x2000)
            if (eraseSectorIntel(smallAddr)) return -1;
        return 0;
    }
    if (flash_kind() == KIND_22XX) return eraseSector22XX(addr);
    return -1;
}

/* writeByWord() for one block: words[] go to byte offset `byteOff`; returns -1 or the failing word index */
int flash_write_block(u32 byteOff, const u16 *words, u32 count)
{
    int intel = flash_kind() == KIND_INTEL;
    u32 base = byteOff >> 1;
    for (u32 i = 0; i < count; i++) {
        u16 w = words[i];
        if (w == 0xFFFF) continue;                    /* already erased */
        if (intel ? writeWordIntel(base + i, w) : writeWord22XX(base + i, w)) return (int)i;
    }
    return -1;
}

/* read the block back; returns -1 or the first mismatching word index */
int flash_verify_block(u32 byteOff, const u16 *words, u32 count, u16 *got)
{
    u32 base = byteOff >> 1;
    for (u32 i = 0; i < count; i++) {
        u16 v = read_word_rom(base + i);
        if (v != words[i]) { *got = v; return (int)i; }
    }
    return -1;
}

const char *flash_manufacturer(u8 id)        /* getManufacturByID() */
{
    switch (id) {
        case 0x52: return "Alliance";
        case 0x1F: return "Atmel";
        case 0x04: return "Fujitsu";
        case 0x89: return "Intel";
        case 0xC2: return "Macronix";
        case 0x62: return "Sanyo";
        case 0xB0: return "Sharp";
        case 0x01: return "Spansion";
        case 0xBF: return "SST";
        case 0x20: return "ST";
        case 0xDA: return "Winbond";
        case 0x1C: return "Mitsubishi";
        default:   return "Unknown";
    }
}
