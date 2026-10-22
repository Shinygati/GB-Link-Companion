@ handoff.S - starts Emerald from inside AgbMain (past the line that clears EWRAM), with the
@ payload already chained onto the game's interrupt handler. The save isn't touched.
@
@ From gen3-poke-multiplayer by Wired-Ariel (hw/mbstub/handoff.S, GPL-3.0), which works this out
@ from pret's pokeemerald (src/crt0.s, src/main.c) and has run it on real hardware. Its steps:
@   1. IRQs off   2. the game's stacks   3. RegisterRamReset(everything but EWRAM)
@   4. clear EWRAM up to the payload   5. INTR_VECTOR = IntrMain
@   6. AgbMain's first lines (InitGpuRegManager, WAITCNT, InitKeys, InitIntrHandlers)
@   7. chain the payload: it gets the IRQ vector, and the game's handler is kept at payload+4
@   8. into AgbMain, after bl InitIntrHandlers
#include "boot_syms.h"

    .section .handoff, "ax"
    .arm
    .align 2
    .global handoff_entry
handoff_entry:
    @ 1. IRQs off: until InitIntrHandlers, REG_IME is the only thing holding them back
    ldr     r1, =REG_IME_ADDR
    mov     r0, #0
    strh    r0, [r1]

    @ 2. the game's stacks (its crt0), before the SWI: RegisterRamReset runs on the current stack
    @    and keeps only the last 0x200 bytes of IWRAM, where these are
    mov     r0, #0x12                       @ IRQ mode
    msr     cpsr_c, r0
    ldr     sp, =GAME_SP_IRQ
    mov     r0, #0x1F                       @ System mode
    msr     cpsr_c, r0
    ldr     sp, =GAME_SP_SYS

    @ 3. RegisterRamReset(0xFE): everything but EWRAM
    mov     r0, #0xFE
    svc     #0x010000

    @ 4. the game's EWRAM to zero, our part excluded
    ldr     r0, =0x02000000
    ldr     r1, =PAYLOAD_BASE
    mov     r2, #0
    mov     r3, #0
    mov     r4, #0
    mov     r5, #0
1:  stmia   r0!, {r2-r5}
    cmp     r0, r1
    blo     1b

    @ 5. INTR_VECTOR = IntrMain in ROM, as the game's crt0 does
    ldr     r0, =ADDR_INTR_MAIN
    ldr     r1, =INTR_VECTOR
    str     r0, [r1]

    @ 6. AgbMain's first lines: BG_PLTT[0] = white, InitGpuRegManager, WAITCNT, InitKeys,
    @    InitIntrHandlers (which turns IRQs back on)
    ldr     r0, =BG_PLTT_ADDR
    ldr     r1, =RGB_WHITE_VALUE
    strh    r1, [r0]
    ldr     r0, =(ADDR_INIT_GPU_REG_MGR + 1)
    mov     lr, pc
    bx      r0
    ldr     r0, =REG_WAITCNT_ADDR
    ldr     r1, =REG_WAITCNT_VALUE
    strh    r1, [r0]
    ldr     r0, =(ADDR_INIT_KEYS + 1)
    mov     lr, pc
    bx      r0
    ldr     r0, =(ADDR_INIT_INTR_HANDLERS + 1)
    mov     lr, pc
    bx      r0

    @ 7. the hook: INTR_VECTOR is now IntrMain_Buffer. Keep it at payload+4 and point the
    @    vector at the payload, with IRQs off between the two writes
    ldr     r1, =REG_IME_ADDR
    mov     r0, #0
    strh    r0, [r1]
    ldr     r1, =INTR_VECTOR
    ldr     r0, [r1]
    ldr     r2, =PAYLOAD_BASE
    str     r0, [r2, #PAYLOAD_OFF_ORIG]
    str     r2, [r1]
    ldr     r1, =REG_IME_ADDR
    mov     r0, #1
    strh    r0, [r1]

    @ 8. into AgbMain, with the stack as its prologue would have left it
    sub     sp, sp, #AGBMAIN_PROLOGUE_BYTES
    ldr     r0, =(ADDR_AGBMAIN_AFTER_INTR + 1)
    bx      r0

    .ltorg
    .global handoff_end
handoff_end:
