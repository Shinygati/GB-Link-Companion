@ the payload, built separately (payload.bin), carried inside the stub
    .section .rodata
    .align 4
    .global payload_bin, payload_bin_end
payload_bin:
    .incbin "payload.bin"
    .align 4
payload_bin_end:
