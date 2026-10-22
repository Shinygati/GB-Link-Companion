@ crt0.S - the multiboot stub's start (after gen3-poke-multiplayer's hw/mbstub/crt0.S, GPL-3.0).
@ The first 0xC0 bytes are the GBA header (gbafix fills in the logo and checksum); the BIOS starts
@ a multiboot program at 0x020000C0.
    .section .header, "ax"
    .arm
    .global _vector
_vector:
    b       _start
    .space  0xBC

    .section .text.start, "ax"
    .arm
    .global _start
_start:
    mov     r0, #0x12               @ IRQ mode stack (unused here, but valid)
    msr     cpsr_c, r0
    ldr     sp, =__sp_irq
    mov     r0, #0x1F               @ System mode: where the C runs
    msr     cpsr_c, r0
    ldr     sp, =__sp_usr
    ldr     r0, =__bss_start        @ .bss to zero
    ldr     r1, =__bss_end
    mov     r2, #0
1:  cmp     r0, r1
    strlo   r2, [r0], #4
    blo     1b
    ldr     r0, =main
    bx      r0
2:  b       2b
    .pool
