/* The multiboot stub: runs in the game's EWRAM until it starts the game. handoff.S has to survive
 * clearing that, so it runs from the top of EWRAM (VMA there, LMA in the image; stub.c copies it).
 * After gen3-poke-multiplayer's hw/mbstub/mbstub.ld (GPL-3.0). */
ENTRY(_start)
MEMORY
{
    ewram (rwx) : ORIGIN = 0x02000000, LENGTH = 0x3C000
    tail  (rwx) : ORIGIN = 0x0203FE00, LENGTH = 0x200
}
SECTIONS
{
    .header : { KEEP(*(.header)) } > ewram
    .text : {
        KEEP(*(.text.start))
        *(.text*)
        *(.rodata*)
        . = ALIGN(4);
    } > ewram
    .data : { *(.data*) . = ALIGN(4); } > ewram
    .handoff : {
        . = ALIGN(4);
        __handoff_start = .;
        KEEP(*(.handoff))
        . = ALIGN(4);
        __handoff_end = .;
    } > tail AT> ewram
    __handoff_lma = LOADADDR(.handoff);
    .bss (NOLOAD) : {
        __bss_start = .;
        *(.bss*)
        *(COMMON)
        . = ALIGN(4);
        __bss_end = .;
    } > ewram
    __sp_irq = 0x03007F00;
    __sp_usr = 0x03007E00;
    /DISCARD/ : { *(.comment) *(.ARM.attributes) *(.note*) }
}
