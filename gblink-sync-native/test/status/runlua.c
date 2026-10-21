/* Runs a GBA ROM in mGBA (libmgba) with a Lua script loaded the way the mGBA frontend does:
 * emu, console, socket and callbacks, with "frame" fired after every frame.
 * Usage: runlua ROM SCRIPT FRAMES */
#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
#include <mgba-util/table.h>
#include <mgba/core/core.h>
#include <mgba/core/log.h>
#include <mgba/core/scripting.h>
#include <mgba/script/context.h>
#include <mgba-util/vfs.h>

static void logcb(struct mLogger *l, int cat, enum mLogLevel lvl, const char *fmt, va_list a)
{
    (void)l;
    if (lvl & (mLOG_ERROR | mLOG_FATAL | mLOG_INFO | mLOG_WARN)) {
        vfprintf(stderr, fmt, a);
        fputc('\n', stderr);
    }
}

int main(int argc, char **argv)
{
    if (argc < 4) return 2;
    struct mLogger logger = { .log = logcb };
    mLogSetDefaultLogger(&logger);
    struct mCore *core = mCoreFind(argv[1]);
    if (!core || !core->init(core)) { fprintf(stderr, "no core\n"); return 1; }
    mCoreInitConfig(core, NULL);
    static color_t video[256 * 224];
    core->setVideoBuffer(core, video, 256);
    if (!mCoreLoadFile(core, argv[1])) { fprintf(stderr, "can't load ROM\n"); return 1; }
    core->reset(core);

    struct mScriptContext ctx;
    mScriptContextInit(&ctx);
    mScriptContextAttachStdlib(&ctx);
    mScriptContextAttachSocket(&ctx);
    mScriptContextAttachCore(&ctx, core);
    mScriptContextAttachLogger(&ctx, &logger);
    mScriptContextRegisterEngines(&ctx);
    struct VFile *vf = VFileOpen(argv[2], O_RDONLY);
    struct mScriptEngineContext *lua = HashTableLookup(&ctx.engines, "lua");
    if (!vf || !lua || !lua->load(lua, argv[2], vf)) { fprintf(stderr, "script failed to load\n"); return 1; }
    if (!lua->run(lua)) { fprintf(stderr, "script failed: %s\n", lua->getError ? lua->getError(lua) : "?"); return 1; }
    mScriptContextTriggerCallback(&ctx, "start");
    int frames = atoi(argv[3]);
    for (int i = 0; i < frames; i++) {
        core->runFrame(core);
        mScriptContextTriggerCallback(&ctx, "frame");
        if (getenv("THROTTLE_US")) usleep(atoi(getenv("THROTTLE_US")));
    }
    fprintf(stderr, "ran %d frames, keys now %03x\n", frames, core->getKeys(core));
    mScriptContextDeinit(&ctx);
    core->deinit(core);
    return 0;
}
