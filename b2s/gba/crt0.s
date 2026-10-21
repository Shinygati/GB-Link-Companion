@ Multiboot start-up: the BIOS loads the whole image to 0x02000000 and jumps to 0x020000C0.
    .section .crt0, "ax"
    .arm
    .global _start
_start:
    b       start_vector
    .fill   156, 1, 0                   @ Nintendo logo (filled in by gbafix)
    .ascii  "BURN2SLOT   "              @ title
    .ascii  "B2SE"                      @ game code
    .ascii  "01"                        @ maker
    .byte   0x96, 0x00, 0x00            @ fixed value, unit code, device type
    .fill   7, 1, 0
    .byte   0x00                        @ version
    .byte   0x00                        @ header checksum (gbafix)
    .fill   2, 1, 0
    b       start_vector                @ 0xC0: multiboot RAM entry point
    .byte   0, 0                        @ boot mode, slave id (written by the BIOS)
    .fill   26, 1, 0
    b       start_vector                @ 0xE0: joybus entry point

start_vector:
    mov     r0, #0x12                   @ IRQ mode stack
    msr     cpsr_c, r0
    ldr     sp, =0x03007FA0
    mov     r0, #0x1F                   @ system mode stack
    msr     cpsr_c, r0
    ldr     sp, =0x03007F00

    ldr     r0, =__iwram_lma            @ copy IWRAM code
    ldr     r1, =__iwram_start
    ldr     r2, =__iwram_end
1:  cmp     r1, r2
    ldrlo   r3, [r0], #4
    strlo   r3, [r1], #4
    blo     1b

    ldr     r1, =__bss_start            @ clear .bss
    ldr     r2, =__bss_end
    mov     r3, #0
2:  cmp     r1, r2
    strlo   r3, [r1], #4
    blo     2b

    ldr     r0, =main
    mov     lr, pc
    bx      r0
3:  b       3b
    .pool
