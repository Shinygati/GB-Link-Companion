/* payload.c - Wonder Cards for retail English Pokemon Emerald on a real GBA, through GB-Link.
 *
 * Runs inside the game: the stub (stub.c) puts it in spare EWRAM before Emerald starts, and
 * handoff.S chains hook.S onto the game's interrupt handler, so payload_irq() runs first on every
 * interrupt. It does two things:
 *
 *  - The link port. While you're in the overworld it takes the port in normal mode (32 bits, the
 *    GB-Link adapter's clock) and answers every word with its status. GB-Link Companion sends a
 *    card as one packet; a word arrives as a serial interrupt, handled here, and never reaches the
 *    game. Anywhere else (battles, menus, the Cable Club) the port is left alone.
 *  - The card. What GB-Link Companion's mGBA script does (gblink_controller.lua, "Wonder Cards"),
 *    the same way: "add" is what Mystery Gift does when it receives a card (the deliveryman on
 *    Pokemon Center 2F hands it over); "use now" runs the card's script straight away where you
 *    stand, then puts back the card and script the game had.
 *
 * Link protocol (the adapter clocks; each reply is the word armed after the previous one):
 *   app -> GBA  0x00000000                           poll
 *               0x5743nnnn, n words, checksum          a packet (checksum: see rx())
 *   GBA -> app  0xA5 | state << 16 | id << 8 | code   status
 *     state: 1 game loaded, 2 free to move (overworld, no script or menu)
 *     id/code: the last packet's id and what happened to it (CODE_* below)
 *   Packet body: word 0 = how ('A' add, 'U' use now) << 24 | id << 16 | resident << 8, then the
 *   card (332 bytes), its icon species (2 bytes, padded to 4) and its RAM script (1000 bytes).
 *   resident: 0 a one-off card, 1 a card that keeps running, 2 the same but replacing the one on. */
typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef volatile u8 vu8;
typedef volatile u16 vu16;
typedef volatile u32 vu32;

#define REG_IE        (*(vu16 *)0x04000200)
#define REG_IME       (*(vu16 *)0x04000208)
#define REG_IF        (*(vu16 *)0x04000202)
#define REG_SIOCNT    (*(vu16 *)0x04000128)
#define REG_SIODATA32 (*(vu32 *)0x04000120)
#define REG_RCNT      (*(vu16 *)0x04000134)
#define IRQ_VBLANK    0x0001
#define IRQ_SERIAL    0x0080
#define SIO_NORMAL32  0x1000
#define SIO_IRQ       0x4000
#define SIO_START     0x0080

/* retail English Emerald (BPEE rev 0): the same addresses as the mGBA script's table */
#define SB1_PTR        0x03005D8C
#define SB2_PTR        0x03005D90
#define MAIN           0x030022C0   /* gMain: callback1, callback2 */
#define SCRIPT_CTX     0x03000E40
#define SCRIPT_STATUS  0x03000E38
#define LOCK_CONTROLS  0x03000F2C
#define CMD_TABLE      0x081DB67C
#define CMD_TABLE_END  0x081DBA08
#define RAM_SCRIPT_RET 0x020375C0
#define INTR_TABLE     0x03002710
#define CB1_OVERWORLD  0x08085E04
#define CB2_OVERWORLD  0x08085E5C
#define SCRIPT_STUB    0x081DCDD2   /* "release, end": where a card's script goes when it's done */
/* the game's own link (from gen3-poke-multiplayer's game_syms.h): OpenLink sets gLinkCallback, and
 * gReceivedRemoteLinkPlayers is 1 while a link session is up */
#define LINK_CALLBACK  0x03003140
#define LINK_REMOTE    0x03003124
/* in SaveBlock1 */
#define SB1_MG         0x322C       /* struct MysteryGiftSave */
#define SB1_RS         0x3728       /* struct RamScript */
#define SB1_FLAGS      0x1270
#define SB1_VARS       0x139C
#define GIFT_FLAG_LO   0x1E4
#define GIFT_FLAG_HI   0x1F3
#define GIFT_VAR_LO    0x40DD
#define GIFT_VAR_HI    0x40E4
#define MG_CARD_CRC    0x1C0
#define MG_META_CRC    0x310
#define MG_TRAINER_IDS 0x344
#define CARD_BYTES     332
#define META_BYTES     36
#define DATA_BYTES     1000
#define RAM_SCRIPT_MAGIC 51
/* cards that keep running put their code here and hook the V-blank entry of gIntrTable */
#define RESIDENT_LO    0x0203FC00
#define RESIDENT_HI    0x02040000
#define HOOK_STATE     0x0203FF60

enum {
    CODE_NONE = 0, CODE_WAITING = 1, CODE_RUNNING = 2,
    CODE_ADDED = 0x10, CODE_USED = 0x11, CODE_RESIDENT_ON = 0x12,
    ERR_BAD_CARD = 0x20, ERR_NO_GAME = 0x21, ERR_NO_SCRIPT = 0x22, ERR_JOB_BUSY = 0x23, ERR_TIMEOUT = 0x24,
    ERR_RESIDENT_UNKNOWN = 0x25, ERR_CHECKSUM = 0x26, ERR_UNKNOWN = 0x27,
};

#define BODY_WORDS (1 + CARD_BYTES / 4 + 1 + DATA_BYTES / 4)   /* 335 */

u32 payload_stack[256] __attribute__((aligned(8)));            /* hook.S runs us on this */

static u8 own, weSetIE, linkBusy;
static u32 gameSerialIntr;                 /* the game's serial entry in gIntrTable, while it's ours */
static u16 gameSiocnt, gameRcnt;           /* the port as the game left it, put back when we let go */
static u32 staleLinkCb;                    /* a gLinkCallback left over from a link that has ended */
static u32 rxBuf[BODY_WORDS], rxCk;
static u16 rxN, rxGot, rxIdle;
static u8 rxOn;
static u16 sinceWord;                      /* frames since the last word went through */
static u32 cmdBuf[BODY_WORDS];
static u8 pending, lastAccepted = 0xFF;
static u8 resId, resCode;                  /* reported in the status */
static u8 freeNow, gameLoaded;

static u8 jobState;                        /* 0 none, 1 waiting for the player to be free, 2 running */
static u8 jobId, jobResident, jobReplace;
static u16 jobCrc;
static u32 jobWait, jobFrames;
static u8 savedRs[4 + DATA_BYTES], savedMg[MG_TRAINER_IDS + 40 - MG_CARD_CRC];
static u16 residentCrc;                    /* the card that keeps running (its script's CRC), 0 = none */
static u32 origVBlank;                     /* the game's own V-blank handler */
static u16 crcTab[256];

static u32 r32(u32 a) { return *(vu32 *)a; }
static u8 r8(u32 a) { return *(vu8 *)a; }
static void w32(u32 a, u32 v) { *(vu32 *)a = v; }
static void w16(u32 a, u16 v) { *(vu16 *)a = v; }
static void w8(u32 a, u8 v) { *(vu8 *)a = v; }
static void copyIn(u32 a, const u8 *s, u32 n) { for (u32 i = 0; i < n; i++) w8(a + i, s[i]); }
static void copyOut(u8 *d, u32 a, u32 n) { for (u32 i = 0; i < n; i++) d[i] = r8(a + i); }
static void zero(u32 a, u32 n) { for (u32 i = 0; i < n; i++) w8(a + i, 0); }
static void put32(u32 a, u32 v) { w8(a, v); w8(a + 1, v >> 8); w8(a + 2, v >> 16); w8(a + 3, v >> 24); }

static u16 crc16(const u8 *b, u32 n)       /* the games' CalcCRC16WithTable */
{
    u16 crc = 0x1121;
    for (u32 i = 0; i < n; i++) {
        u16 hi = crc >> 8;
        crc ^= b[i];
        crc = hi ^ crcTab[crc & 0xFF];
    }
    return ~crc;
}

static void result(u8 id, u8 code) { resId = id; resCode = code; }

static u32 status(void)
{
    return 0xA5000000u | (u32)(freeNow ? 2 : gameLoaded ? 1 : 0) << 16 | (u32)resId << 8 | resCode;
}

/* SaveBlock1, if a game is loaded (else 0) */
static u32 saveBlock(void)
{
    u32 sb1 = r32(SB1_PTR), sb2 = r32(SB2_PTR);
    if (sb1 < 0x02000000 || sb1 >= 0x02040000 || sb2 < 0x02000000 || sb2 >= 0x03008000) return 0;
    if (*(vu16 *)(sb2 + 0x0A) == 0 && *(vu16 *)(sb2 + 0x0C) == 0) return 0;   /* no Trainer ID yet */
    return sb1;
}

/* the player can move: the overworld is running and no script or menu has the controls */
static int playerFree(void)
{
    return (r32(MAIN + 4) | 1) == (CB2_OVERWORLD | 1) && (r32(MAIN) | 1) == (CB1_OVERWORLD | 1) &&
           r8(SCRIPT_STATUS) == 2 && r8(LOCK_CONTROLS) == 0;
}

static int hooked(void)
{
    u32 v = r32(INTR_TABLE + 0x10);
    return v >= RESIDENT_LO && v < RESIDENT_HI;
}

/* ------------------------------------------------------------------------------- link port */
static void rx(u32 w)
{
    if (!rxOn) {
        u32 n = w & 0xFFFF;
        if ((w >> 16) == 0x5743 && n == BODY_WORDS) { rxOn = 1; rxN = n; rxGot = 0; rxCk = 0x5743; rxIdle = 0; }
        return;
    }
    rxIdle = 0;
    if (rxGot < rxN) {
        rxBuf[rxGot++] = w;
        rxCk = ((rxCk << 1) | (rxCk >> 31)) ^ w;
        return;
    }
    rxOn = 0;
    u8 id = rxBuf[0] >> 16;
    if (w != rxCk) { result(id, ERR_CHECKSUM); return; }
    if (id == lastAccepted) return;                    /* sent again (the app missed our answer) */
    if (pending) { result(id, ERR_JOB_BUSY); return; }
    for (u32 i = 0; i < BODY_WORDS; i++) cmdBuf[i] = rxBuf[i];
    pending = 1;
    lastAccepted = id;
    result(id, CODE_WAITING);
}

static void serial(void)
{
    u32 w = REG_SIODATA32;
    REG_IF = IRQ_SERIAL;
    sinceWord = 0;
    rx(w);
    REG_SIODATA32 = status();
    REG_SIOCNT = SIO_NORMAL32 | SIO_IRQ | SIO_START;
}

/* While the port is ours, the serial entry of the game's gIntrTable is ours too: a word that
 * arrives just as hook.S hands an interrupt over to the game's IntrMain is then still ours (IntrMain
 * would otherwise give it to the game's SerialIntr, and the port would stay deaf). IntrMain calls
 * it with interrupts on: keep them off while the word is handled. */
static void serialFromGame(void)
{
    u16 ime = REG_IME;
    REG_IME = 0;
    if (own) serial();
    REG_IME = ime;
}

/* Start the port over from the first bit, the way the GB-Link Companion helper does it on hardware
 * (b2s/gba/sio_gba.c, sio_resync_arm): stop it, switch the link port to general-purpose mode (which
 * resets the serial unit), wait until the clock line has been still for a moment (we're between
 * words, if the adapter is sending at all), then serial mode again and listen. */
static void resync(void)
{
    REG_SIOCNT = SIO_NORMAL32;
    REG_RCNT = 0x8000;                             /* general purpose: bit 0 reads the clock line */
    u16 level = REG_RCNT & 1;
    for (u32 still = 0, n = 0; still < 64 && n < 20000; n++) {   /* ~15 us still, at most ~2 ms */
        u16 now = REG_RCNT & 1;
        if (now != level) { level = now; still = 0; } else still++;
    }
    REG_RCNT = 0;
    REG_SIOCNT = SIO_NORMAL32;
    REG_SIODATA32 = status();
    REG_SIOCNT = SIO_NORMAL32 | SIO_IRQ | SIO_START;
}

static void takePort(void)
{
    gameSiocnt = REG_SIOCNT & ~(SIO_IRQ | SIO_START);
    gameRcnt = REG_RCNT;
    sinceWord = 0;
    resync();
    if (!(REG_IE & IRQ_SERIAL)) { REG_IE |= IRQ_SERIAL; weSetIE = 1; }
    gameSerialIntr = r32(INTR_TABLE + 4);
    w32(INTR_TABLE + 4, (u32)serialFromGame);
    own = 1;
    rxOn = 0;
}

static void giveBackSerialEntry(void)
{
    if (gameSerialIntr) { w32(INTR_TABLE + 4, gameSerialIntr); gameSerialIntr = 0; }
}

static void releasePort(int ours)
{
    if (ours) {                                    /* the game's own settings back, without our interrupt */
        REG_SIOCNT = gameSiocnt;
        REG_RCNT = gameRcnt;
    }
    if (weSetIE) { REG_IE &= ~IRQ_SERIAL; weSetIE = 0; }
    giveBackSerialEntry();
    own = 0;
    rxOn = 0;
}

/* ----------------------------------------------------------------------------------- cards */
/* What Mystery Gift does when it receives a card: ClearSavedWonderCardAndRelated, SaveWonderCard
 * and InitRamScript_NoObjectEvent. */
static void addCard(u32 sb1, const u8 *card, const u8 *icon, const u8 *data)
{
    u32 mg = sb1 + SB1_MG;
    put32(mg + MG_CARD_CRC, crc16(card, CARD_BYTES));
    copyIn(mg + MG_CARD_CRC + 4, card, CARD_BYTES);
    u8 meta[META_BYTES];
    for (u32 i = 0; i < META_BYTES; i++) meta[i] = 0;
    meta[6] = icon[0]; meta[7] = icon[1];                  /* iconSpecies */
    put32(mg + MG_META_CRC, 0);
    copyIn(mg + MG_META_CRC + 4, meta, META_BYTES);
    zero(mg + MG_TRAINER_IDS, 40);
    for (u32 f = GIFT_FLAG_LO; f <= GIFT_FLAG_HI; f++) {   /* ClearMysteryGiftFlags */
        u32 a = sb1 + SB1_FLAGS + (f >> 3);
        w8(a, r8(a) & ~(1 << (f & 7)));
    }
    for (u32 v = GIFT_VAR_LO; v <= GIFT_VAR_HI; v++) w16(sb1 + SB1_VARS + (v - 0x4000) * 2, 0);   /* ClearMysteryGiftVars */
    if (data[0] == RAM_SCRIPT_MAGIC) {
        put32(sb1 + SB1_RS, crc16(data, DATA_BYTES));
        copyIn(sb1 + SB1_RS + 4, data, DATA_BYTES);
    } else {
        zero(sb1 + SB1_RS, 4 + DATA_BYTES);
    }
}

/* Using a card now: its card and script go where the deliveryman's would, the game's script
 * engine starts on the script (ScriptContext_SetupScript), and afterwards the game's own are put back. */
static void startJob(u32 sb1)
{
    const u8 *card = (const u8 *)&cmdBuf[1], *data = (const u8 *)&cmdBuf[1 + CARD_BYTES / 4 + 1];
    if (jobResident && !hooked()) origVBlank = r32(INTR_TABLE + 0x10);
    if (jobReplace && hooked() && origVBlank) {           /* take the old card's hook away */
        w32(INTR_TABLE + 0x10, origVBlank);
        zero(HOOK_STATE, RESIDENT_HI - HOOK_STATE);
        residentCrc = 0;
    }
    u32 mg = sb1 + SB1_MG, rs = sb1 + SB1_RS;
    copyOut(savedRs, rs, sizeof savedRs);
    copyOut(savedMg, mg + MG_CARD_CRC, sizeof savedMg);
    put32(mg + MG_CARD_CRC, crc16(card, CARD_BYTES));
    copyIn(mg + MG_CARD_CRC + 4, card, CARD_BYTES);
    put32(rs, crc16(data, DATA_BYTES));
    copyIn(rs + 4, data, DATA_BYTES);
    w32(RAM_SCRIPT_RET, SCRIPT_STUB);
    zero(SCRIPT_CTX, 0x74);                                /* InitScriptContext */
    w32(SCRIPT_CTX + 0x5C, CMD_TABLE);
    w32(SCRIPT_CTX + 0x60, CMD_TABLE_END);
    w32(SCRIPT_CTX + 8, rs + 8);                           /* SetupBytecodeScript: the script */
    w8(SCRIPT_CTX + 1, 1);                                 /* SCRIPT_MODE_BYTECODE */
    w8(LOCK_CONTROLS, 1);                                  /* LockPlayerFieldControls */
    w8(SCRIPT_STATUS, 0);                                  /* CONTEXT_RUNNING */
    jobState = 2;
    jobFrames = 0;
    result(jobId, CODE_RUNNING);
}

static void finishJob(void)
{
    u32 sb1 = saveBlock();
    if (sb1) {                                             /* the game's own card and script back */
        copyIn(sb1 + SB1_MG + MG_CARD_CRC, savedMg, sizeof savedMg);
        copyIn(sb1 + SB1_RS, savedRs, sizeof savedRs);
    }
    if (jobResident) residentCrc = hooked() ? jobCrc : 0;
    jobState = 0;
    result(jobId, CODE_USED);
}

static void command(void)
{
    u32 head = cmdBuf[0];
    u8 how = head >> 24, id = head >> 16, res = head >> 8;
    const u8 *card = (const u8 *)&cmdBuf[1], *icon = (const u8 *)&cmdBuf[1 + CARD_BYTES / 4];
    const u8 *data = (const u8 *)&cmdBuf[1 + CARD_BYTES / 4 + 1];
    pending = 0;
    u32 sb1 = saveBlock();
    if (!sb1) { result(id, ERR_NO_GAME); return; }
    if (how == 'A') { addCard(sb1, card, icon, data); result(id, CODE_ADDED); return; }
    if (how != 'U') { result(id, ERR_UNKNOWN); return; }
    if (data[0] != RAM_SCRIPT_MAGIC) { result(id, ERR_NO_SCRIPT); return; }
    if (jobState) { result(id, ERR_JOB_BUSY); return; }
    u16 crc = crc16(data, DATA_BYTES);
    if (res && hooked() && residentCrc != crc) {           /* another card that keeps running is on */
        if (res == 1) { result(id, CODE_RESIDENT_ON); return; }
        if (!origVBlank) { result(id, ERR_RESIDENT_UNKNOWN); return; }
    }
    jobId = id; jobResident = res != 0; jobReplace = res == 2; jobCrc = crc;
    jobState = 1; jobWait = 0;
    result(id, CODE_WAITING);
}

static void tick(void)
{
    if (!crcTab[1]) {
        for (u32 i = 0; i < 256; i++) {
            u32 c = i;
            for (int k = 0; k < 8; k++) c = (c & 1) ? (c >> 1) ^ 0x8408 : c >> 1;
            crcTab[i] = c;
        }
    }
    u32 sb1 = saveBlock();
    gameLoaded = sb1 != 0;
    freeNow = gameLoaded && playerFree();
    if (gameLoaded && !origVBlank && !hooked()) origVBlank = r32(INTR_TABLE + 0x10);

    if (pending) command();
    if (jobState == 1) {
        if (++jobWait > 3 * 60 * 60) { jobState = 0; result(jobId, ERR_TIMEOUT); }
        else if (sb1 && freeNow) startJob(sb1);
    } else if (jobState == 2) {
        ++jobFrames;
        if ((r8(SCRIPT_STATUS) == 2 && r8(LOCK_CONTROLS) == 0) || jobFrames > 36000) finishJob();
    }

    /* the game's own link (Cable Club, Union Room, ...): it starts with OpenLink setting
     * gLinkCallback; it's over when the game has turned its serial interrupt off and no one is linked.
     * (After a cancelled link the callback stays set: that one is remembered as stale.) */
    u32 lcb = r32(LINK_CALLBACK);
    int linkCb = (lcb >> 24) == 0x08 && (lcb & 1) && lcb != staleLinkCb;
    if (!linkBusy && (linkCb || r8(LINK_REMOTE) == 1)) {
        linkBusy = 1;
        if (own) { own = 0; weSetIE = 0; rxOn = 0; giveBackSerialEntry(); }   /* the registers are already the game's */
    } else if (linkBusy && !(REG_IE & IRQ_SERIAL) && r8(LINK_REMOTE) == 0) {
        linkBusy = 0;
        staleLinkCb = lcb;
    }
    if (lcb != staleLinkCb && !((lcb >> 24) == 0x08 && (lcb & 1))) staleLinkCb = 0;

    /* the port: ours in the overworld (and while a card runs), the game's anywhere else */
    int want = gameLoaded && !linkBusy && ((r32(MAIN + 4) | 1) == (CB2_OVERWORLD | 1) || jobState == 2);
    if (own && (REG_SIOCNT & 0x3001) != SIO_NORMAL32) {
        own = 0; weSetIE = 0; rxOn = 0; giveBackSerialEntry();   /* the game set the port up for itself */
    } else if (own && !want) {
        releasePort(1);
    } else if (!own && want) {
        takePort();
    }
    if (own) {
        if (rxOn && ++rxIdle > 20) rxOn = 0;               /* a packet that stopped half way */
        /* Half a second with no word: start listening again from the first bit. A link cable pulled
         * out (or pushed in) in the middle of a word leaves the port counting from there, so every
         * word after it would be split across two; the app pauses when it hears nonsense, and this
         * puts the GBA back in step. */
        if (++sinceWord >= 30) {
            sinceWord = 0;
            rxOn = 0;
            resync();
        }
        if (!(REG_IE & IRQ_SERIAL)) { REG_IE |= IRQ_SERIAL; weSetIE = 1; }
        if (r32(INTR_TABLE + 4) != (u32)serialFromGame) {   /* the game put its own back */
            gameSerialIntr = r32(INTR_TABLE + 4);
            w32(INTR_TABLE + 4, (u32)serialFromGame);
        }
        if (!(REG_SIOCNT & SIO_START) && !(REG_IF & IRQ_SERIAL))   /* a word went by unseen: listen again */
            REG_SIOCNT = SIO_NORMAL32 | SIO_IRQ | SIO_START;
        if (!rxOn && (REG_SIOCNT & SIO_START)) REG_SIODATA32 = status();   /* keep the answer current */
    }
}

/* every interrupt, before the game's handler (hook.S) */
void payload_irq(void)
{
    u16 f = REG_IF & REG_IE;
    if ((f & IRQ_SERIAL) && own) serial();
    if (f & IRQ_VBLANK) tick();
    while (own && (REG_IF & REG_IE & IRQ_SERIAL)) serial();   /* one that came in meanwhile */
}
