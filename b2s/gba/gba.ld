OUTPUT_ARCH(arm)
ENTRY(_start)
MEMORY {
    ewram (rwx) : ORIGIN = 0x02000000, LENGTH = 256K
    iwram (rwx) : ORIGIN = 0x03000000, LENGTH = 32K - 0x200
}
SECTIONS {
    .text : {
        KEEP(*(.crt0))
        *(.text .text.*)
        *(.glue_7 .glue_7t .vfp11_veneer .v4_bx)
        *(.rodata .rodata.*)
        *(.data .data.*)
        . = ALIGN(4);
    } > ewram
    .iwram : {
        __iwram_start = .;
        *(.iwram .iwram.*)
        . = ALIGN(4);
        __iwram_end = .;
    } > iwram AT > ewram
    __iwram_lma = LOADADDR(.iwram);
    __image_end = __iwram_lma + SIZEOF(.iwram);
    .bss (NOLOAD) : {
        . = ALIGN(4);
        __bss_start = .;
        *(.bss .bss.* COMMON)
        . = ALIGN(4);
        __bss_end = .;
    } > ewram
    /DISCARD/ : { *(.ARM.exidx*) *(.ARM.attributes) *(.comment) }
}
