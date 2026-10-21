#ifndef FLASH_H
#define FLASH_H
#include "gba.h"

typedef struct {
    u8  busType;
    union { u8 intelType; u8 adressSeqType; };
    u16 flashid;
    u16 manufactorID;
    u16 bufferSize;
    u32 size;
} CartInfo;

extern CartInfo cart;

int  flash_detect(void);
int  flash_kind(void);
int  flash_erase_step(u32 wordAddr);
int  flash_write_block(u32 byteOff, const u16 *words, u32 count);
int  flash_verify_block(u32 byteOff, const u16 *words, u32 count, u16 *got);
const char *flash_manufacturer(u8 id);
#endif
