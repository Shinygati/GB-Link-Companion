/* The payload: from PAYLOAD_BASE (0x0203CF80), below the RAM that Wonder Cards which keep running
 * use (0x0203FC00 up). Its stack is in its own .bss. */
ENTRY(_start)
SECTIONS
{
    . = 0x0203CF80;
    .header : { KEEP(*(.text.start)) }
    .text : { *(.text*) *(.rodata*) . = ALIGN(4); }
    .data : { *(.data*) . = ALIGN(4); }
    .bss (NOLOAD) : { *(.bss*) *(COMMON) . = ALIGN(8); }
    payload_end = .;
    /DISCARD/ : { *(.comment) *(.ARM.attributes) *(.note*) }
}
