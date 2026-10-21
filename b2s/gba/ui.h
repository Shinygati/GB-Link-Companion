#ifndef UI_H
#define UI_H
#include "gba.h"
enum { C_WHITE, C_GREY, C_GREEN, C_RED, C_YELLOW, C_CYAN };
void ui_init(void);
void ui_clear_line(int row);
void ui_text(int row, int col, int color, const char *s);
void ui_hex(int row, int col, int color, u32 v, int digits);
void ui_dec(int row, int col, int color, u32 v);
void ui_bar(int row, u32 done, u32 total);
#endif
