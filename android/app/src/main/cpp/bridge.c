/* GB-Link Android mGBA frontend, GPL-3.0-or-later. */
#include <jni.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include "libretro.h"
extern void companion_prepare_save(int, size_t);
extern int companion_take_save_events(void);
static uint32_t pixels[240*160];
static int16_t samples[8192];
static size_t sampleCount;
static int buttons, saveLength, loaded;
static bool environment(unsigned cmd, void* data) {
    switch(cmd) {
    case RETRO_ENVIRONMENT_SET_PIXEL_FORMAT: return *(int*)data == RETRO_PIXEL_FORMAT_RGB565;
    case RETRO_ENVIRONMENT_GET_CAN_DUPE: *(bool*)data = true; return true;
    case RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE: *(bool*)data = false; return true;
    case RETRO_ENVIRONMENT_GET_CORE_OPTIONS_VERSION: *(unsigned*)data = 0; return true;
    case RETRO_ENVIRONMENT_GET_VARIABLE: ((struct retro_variable*)data)->value = NULL; return false;
    case RETRO_ENVIRONMENT_SET_INPUT_DESCRIPTORS:
    case RETRO_ENVIRONMENT_SET_VARIABLES: return true;
    default: return false;
    }
}
static void video(const void* data, unsigned width, unsigned height, size_t pitch) {
    if (!data || width != 240 || height != 160) return;
    for (unsigned y=0; y<160; y++) {
        const uint16_t* row = (const uint16_t*)((const char*)data + y*pitch);
        for (unsigned x=0;x<240;x++) {
            unsigned p=row[x], r=(p>>11)&31, g=(p>>5)&63, b=p&31;
            pixels[y*240+x]=0xFF000000|((r*255/31)<<16)|((g*255/63)<<8)|(b*255/31);
        }
    }
}
static size_t audio(const int16_t* data, size_t frames) {
    size_t n=frames*2;
    if(n > 8192-sampleCount) n=8192-sampleCount;
    memcpy(samples+sampleCount,data,n*sizeof(int16_t)); sampleCount+=n;
    return frames;
}
static void poll(void) {}
static int16_t input(unsigned port, unsigned device, unsigned index, unsigned id) {
    return port==0 && device==RETRO_DEVICE_JOYPAD && id<16 ? !!(buttons&(1<<id)) : 0;
}
JNIEXPORT jboolean JNICALL Java_io_gblink_companion_Mgba_load(JNIEnv* env,jclass cls,jbyteArray rom,jbyteArray save,jint type) {
    (void)cls;
    if (loaded || type<1 || type>6) return JNI_FALSE;
    jsize romSize=(*env)->GetArrayLength(env,rom), n=(*env)->GetArrayLength(env,save);
    if(romSize<192 || romSize>32*1024*1024 || n>131072) return JNI_FALSE;
    retro_set_environment(environment); retro_set_video_refresh(video);
    retro_set_audio_sample_batch(audio); retro_set_input_poll(poll); retro_set_input_state(input);
    retro_init();
    jbyte* bytes=(*env)->GetByteArrayElements(env,rom,NULL);
    struct retro_game_info game={ .path="cartridge.gba", .data=bytes, .size=romSize };
    bool ok=retro_load_game(&game);
    (*env)->ReleaseByteArrayElements(env,rom,bytes,JNI_ABORT);
    if(!ok){retro_deinit();return JNI_FALSE;}
    saveLength=n;
    void* ram=retro_get_memory_data(RETRO_MEMORY_SAVE_RAM);
    (*env)->GetByteArrayRegion(env,save,0,n,(jbyte*)ram);
    companion_prepare_save(type,n);
    loaded=1; buttons=0;
    return JNI_TRUE;
}
JNIEXPORT jint JNICALL Java_io_gblink_companion_Mgba_step(JNIEnv* env,jclass cls,jint keys,jintArray frame,jshortArray pcm) {
    (void)cls;
    if(!loaded) return 0;
    buttons=keys; sampleCount=0; retro_run();
    (*env)->SetIntArrayRegion(env,frame,0,240*160,(jint*)pixels);
    (*env)->SetShortArrayRegion(env,pcm,0,(jsize)sampleCount,samples);
    return (jint)sampleCount;
}
JNIEXPORT jbyteArray JNICALL Java_io_gblink_companion_Mgba_save(JNIEnv* env,jclass cls) {
    (void)cls; if(!loaded) return NULL;
    jbyteArray b=(*env)->NewByteArray(env,saveLength);
    (*env)->SetByteArrayRegion(env,b,0,saveLength,retro_get_memory_data(RETRO_MEMORY_SAVE_RAM)); return b;
}
JNIEXPORT void JNICALL Java_io_gblink_companion_Mgba_close(JNIEnv* env,jclass cls) {
    (void)env;(void)cls;
    if(loaded){retro_unload_game();retro_deinit();loaded=0;}
}
JNIEXPORT jint JNICALL Java_io_gblink_companion_Mgba_saveEvents(JNIEnv* env,jclass cls) {
    (void)env;(void)cls;return loaded?companion_take_save_events():0;
}
