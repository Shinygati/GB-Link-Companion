@ hook.S - the payload's entry: INTR_VECTOR points here, and the game's own handler is kept at +4.
@ After gen3-poke-multiplayer's payload/hook.S (Wired-Ariel, GPL-3.0): our code runs first, on a
@ stack of its own (the IRQ stack is tiny), then the game's handler takes over with the BIOS's lr.
@ One change: when our code handled every interrupt that was pending (a word on the link port),
@ there's nothing left for the game's IntrMain, and it isn't called (it would find no interrupt).
@
@   +0x00  b hook_entry
@   +0x04  g_origHandler   (set by handoff.S)
@   +0x08  g_magic 'HOOK'  (checked by the stub)
    .section .text.start, "ax"
    .arm
    .global _start
_start:
    b       hook_entry
    .global g_origHandler
g_origHandler:
    .word   0
    .global g_magic
g_magic:
    .word   0x4B4F4F48

    .text
    .arm
    .global hook_entry
hook_entry:
    stmfd   sp!, {lr}               @ the BIOS's lr, for the game's handler
    ldr     r0, =payload_stack + 1024
    mov     r1, sp                  @ the IRQ stack, kept by hook_thumb on ours
    mov     sp, r0
    ldr     r0, =hook_thumb
    mov     lr, pc
    bx      r0                      @ back here in ARM with r1 = the IRQ stack
    mov     sp, r1
    ldmfd   sp!, {lr}
    mov     r0, #0x04000000
    ldr     r2, [r0, #0x200]        @ IE | IF << 16
    and     r2, r2, r2, lsr #16
    ldr     r3, =0x3FFF
    ands    r2, r2, r3
    ldrne   r12, =g_origHandler     @ something left: the game's handler, which returns to the BIOS
    ldrne   r12, [r12]
    bxne    r12
    bx      lr                      @ nothing left: straight back to the BIOS
    .ltorg

    .thumb
    .thumb_func
hook_thumb:
    push    {r1, lr}
    bl      payload_irq
    pop     {r1, r3}
    bx      r3
