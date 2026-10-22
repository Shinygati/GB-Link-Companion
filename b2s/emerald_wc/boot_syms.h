/* boot_syms.h - retail English Pokemon Emerald (BPEE, the one revision sold in the USA and Europe).
 *
 * The start-up addresses and the way into the game come from gen3-poke-multiplayer by Wired-Ariel
 * (https://github.com/Wired-Ariel/gen3-poke-multiplayer, GPL-3.0: hw/mbstub/boot_syms_it.h and
 * boot_syms_usa.h), where they were taken from pret's pokeemerald .map and checked on the ROM and
 * on real hardware. Checked again here against the retail ROM (sha1 f3ae0881...b7). */
#ifndef BOOT_SYMS_H
#define BOOT_SYMS_H

/* the game's start-up code */
#define ADDR_INTR_MAIN           0x08000248  /* IntrMain, in ROM                    */
#define ADDR_INIT_KEYS           0x080005BC
#define ADDR_INIT_INTR_HANDLERS  0x08000684
#define ADDR_INIT_GPU_REG_MGR    0x08000FE4
#define ADDR_AGBMAIN_AFTER_INTR  0x080003CE  /* AgbMain + 0x2A: just after bl InitIntrHandlers */
#define AGBMAIN_PROLOGUE_BYTES   24          /* push {r4-r7,lr} + push {r7}... as AgbMain does  */
#define ADDR_INTR_MAIN_BUFFER    0x03002750
#define INTR_VECTOR              0x03007FFC
#define REG_IME_ADDR             0x04000208
#define REG_WAITCNT_ADDR         0x04000204
#define REG_WAITCNT_VALUE        0x4014
#define BG_PLTT_ADDR             0x05000000
#define RGB_WHITE_VALUE          0x7FFF
#define GAME_SP_SYS              0x03007E40  /* as the game's crt0 sets them        */
#define GAME_SP_IRQ              0x03007FA0

/* our part of EWRAM: the game's EWRAM ends at 0x0203CF64 (pokeemerald's .map). Pokemon cards
 * that keep running put their code at 0x0203FC00-0x02040000, so the payload ends below that. */
#define PAYLOAD_BASE             0x0203CF80
#define PAYLOAD_LIMIT            0x0203FC00
#define EWRAM_END                0x02040000
#define HANDOFF_BASE             0x0203FE00  /* only used while starting the game   */

/* the payload's first words (hook.S) */
#define PAYLOAD_OFF_ORIG         0x04
#define PAYLOAD_OFF_MAGIC        0x08
#define PAYLOAD_MAGIC            0x4B4F4F48  /* 'HOOK' */

/* is the cartridge retail English Emerald? the game code, the two literal-pool words the start-up
 * relies on, and four instructions spread over the ROM (gen3-poke-multiplayer's signatures) */
#define ADDR_GAMECODE            0x080000AC
#define GAMECODE_BPEE            0x45455042
#define ADDR_REVISION            0x080000BC
#define ADDR_POOL_INTRMAIN       0x080006D0
#define ADDR_POOL_INTRBUF        0x080006D4
#define ADDR_CHK1 0x08085E5C /* CB2_Overworld */                         
#define VAL_CHK1  0x4809B510
#define ADDR_CHK2 0x0808DC44 /* SpawnSpecialObjectEventParameterized */
#define VAL_CHK2  0x4646B570
#define ADDR_CHK3 0x080931C0 /* ObjectEventSetHeldMovement */
#define VAL_CHK3  0x1C04B570
#define ADDR_CHK4 0x081AAD5C /* CB2_BagMenuRun */
#define VAL_CHK4  0xF6FEB500

#endif
