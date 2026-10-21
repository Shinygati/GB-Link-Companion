#ifndef SAVE_H
#define SAVE_H
#include "gba.h"
/* save types (also used by the web page) */
#define SAVE_NONE       0
#define SAVE_SRAM32     1
#define SAVE_SRAM64     2
#define SAVE_FLASH64    3
#define SAVE_FLASH128   4
#define SAVE_EEPROM4K   5   /* 512 bytes */
#define SAVE_EEPROM64K  6   /* 8 KB */
u32  save_size(u32 type);
u32  save_detect(u32 *flashId);
int  save_read(u32 type, u32 offset, u32 nbytes, u8 *out);
int  save_write(u32 type, u32 offset, u32 nbytes, const u8 *in, u8 *got);
void save_title(u32 out[4]);
#endif
