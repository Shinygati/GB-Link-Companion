#ifndef GBA_H
#define GBA_H
typedef unsigned char u8;   typedef unsigned short u16;   typedef unsigned int u32;
typedef signed int s32;
typedef volatile u8 vu8;    typedef volatile u16 vu16;    typedef volatile u32 vu32;

#ifndef B2S_SIM
#define REG_DISPCNT   (*(vu16 *)0x04000000)
#define REG_VCOUNT    (*(vu16 *)0x04000006)
#define REG_SIODATA32 (*(vu32 *)0x04000120)
#define REG_SIOCNT    (*(vu16 *)0x04000128)
#define REG_KEYINPUT  (*(vu16 *)0x04000130)
#define REG_RCNT      (*(vu16 *)0x04000134)
#define REG_IE        (*(vu16 *)0x04000200)
#define REG_WAITCNT   (*(vu16 *)0x04000204)
#define REG_IME       (*(vu16 *)0x04000208)
#define REG_TM0CNT_L  (*(vu16 *)0x04000100)
#define REG_TM0CNT_H  (*(vu16 *)0x04000102)
#define REG_TM2CNT_L  (*(vu16 *)0x04000108)
#define REG_TM2CNT_H  (*(vu16 *)0x0400010A)
#define REG_TM3CNT_L  (*(vu16 *)0x0400010C)
#define REG_TM3CNT_H  (*(vu16 *)0x0400010E)
#define VRAM16        ((vu16 *)0x06000000)
#define CART16        ((vu16 *)0x08000000)   /* the GBA cart bus - same as GBAROM on the DS */
#define IWRAM_CODE    __attribute__((section(".iwram"), long_call, target("arm"), noinline))
#else
#define IWRAM_CODE
unsigned short sim_keyinput(void);
#define REG_KEYINPUT  (sim_keyinput())
#endif
#endif
