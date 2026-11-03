/* Android cartridge save adapter; compiled in libretro.c, MPL-2.0. */
#include <mgba/internal/gba/savedata.h>
static int companionSaveEvents;
static void companion_savedata_updated(void* context) { (void)context; companionSaveEvents++; }
int companion_take_save_events(void) { int n=companionSaveEvents; companionSaveEvents=0; return n; }
void companion_prepare_save(int type, size_t size) {
    /* Bind the VFile at the real chip size, rather than libretro's maximum.
       This matters for EEPROM512, EEPROM8K, and SRAM64K. */
    struct VFile* save = VFileFromMemory(savedata, size);
    if (!core->loadSave(core, save)) save->close(save);
    static const int types[] = {0, SAVEDATA_SRAM, SAVEDATA_SRAM512,
        SAVEDATA_FLASH512, SAVEDATA_FLASH1M, SAVEDATA_EEPROM512, SAVEDATA_EEPROM};
    GBASavedataForceType(&((struct GBA*)core->board)->memory.savedata, types[type]);
    deferredSetup = false;
    companionSaveEvents=0;
    struct mCoreCallbacks callbacks={0};
    callbacks.savedataUpdated=companion_savedata_updated;
    core->addCoreCallbacks(core,&callbacks);
}
