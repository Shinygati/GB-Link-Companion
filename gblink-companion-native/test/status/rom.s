@ Test cart for the status script: copies a prepared RAM image from ROM into EWRAM and IWRAM,
@ then advances the RNG word (address at 0x08000400) once per frame like the games do.
    .arm
    .section .text
    .global _start
_start:
    b main
    .space 0xBC            @ header (0x04-0xBF), filled in by mkrom.py
main:
    ldr r0, =0x08100000
    ldr r1, =0x02000000
    ldr r2, =0x10000       @ 256 KB EWRAM in words
1:  ldr r3, [r0], #4
    str r3, [r1], #4
    subs r2, r2, #1
    bne 1b
    ldr r0, =0x08140000
    ldr r1, =0x03000000
    ldr r2, =0x1C00        @ 28 KB of IWRAM in words
2:  ldr r3, [r0], #4
    str r3, [r1], #4
    subs r2, r2, #1
    bne 2b
    ldr r4, =0x08000400
    ldr r4, [r4]           @ RNG address
    ldr r6, =0x41C64E6D
    ldr r7, =0x6073
    ldr r8, =0x04000006    @ VCOUNT
frame:
3:  ldrh r0, [r8]
    cmp r0, #160
    bne 3b
    ldr r5, [r4]
    mla r5, r6, r5, r7
    str r5, [r4]
4:  ldrh r0, [r8]
    cmp r0, #160
    beq 4b
    b frame
    .ltorg
