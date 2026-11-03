/* Original minimal GBA test program. No commercial ROM or BIOS needed. */
    .arm
    .section .text
    .global _start
_start:
    b main
    .space 0xBC
main:
    ldr sp, =0x03007F00
    ldr r0, =0x04000000
    ldr r1, =0x0403
    strh r1, [r0]
    ldr r2, =0x0E000000
    ldrb r3, [r2]
    add r3, r3, #1
    strb r3, [r2]
    ldr r0, =0x06000000
    ldr r1, =0x03E0
    ldr r2, =38400
fill:
    strh r1, [r0], #2
    subs r2, r2, #1
    bne fill
idle:
    b idle
    .ltorg
