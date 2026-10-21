/* Boot the multiboot image in libmgba for N frames and save the screen as PPM. */
#include <mgba/core/core.h>
#include <mgba/core/config.h>
#include <stdio.h>
#include <fcntl.h>
#include <mgba/internal/gba/gba.h>
#include <mgba-util/vfs.h>
#include <mgba/internal/arm/arm.h>
#include <stdlib.h>
int main(int argc, char **argv)
{
    struct mCore *core = mCoreFind(argv[1]);
    if (!core || !core->init(core)) { puts("no core"); return 1; }
    mCoreInitConfig(core, NULL);
    unsigned w, h; core->desiredVideoDimensions(core, &w, &h);
    color_t *buf = calloc(w * h, sizeof(color_t));
    core->setVideoBuffer(core, buf, w);
    struct VFile *vf = VFileOpen(argv[1], O_RDONLY);
    if (!GBALoadMB((struct GBA *)core->board, vf)) { puts("load failed"); return 1; }
    core->reset(core);
    for (int i = 0; i < atoi(argv[3]); i++) core->runFrame(core);
    FILE *f = fopen(argv[2], "wb");
    fprintf(f, "P6\n%u %u\n255\n", w, h);
    for (unsigned i = 0; i < w * h; i++) {
        unsigned c = buf[i];
        unsigned char px[3] = {c & 0xFF, (c >> 8) & 0xFF, (c >> 16) & 0xFF};
        fwrite(px, 1, 3, f);
    }
    fclose(f);
    struct ARMCore *cpu = core->cpu;
    printf("EWRAM[0]=%08x EWRAM[C0]=%08x IWRAM[0]=%08x DISPCNT=%04x VRAM0=%04x PC=%08x\n",
        core->busRead32(core, 0x02000000), core->busRead32(core, 0x020000C0), core->busRead32(core, 0x03000000),
        core->busRead16(core, 0x04000000), core->busRead16(core, 0x06000000), cpu->gprs[15]);
    printf("pc=%08x size %ux%u color_t=%zu\n", 0u, w, h, sizeof(color_t));
    return 0;
}
