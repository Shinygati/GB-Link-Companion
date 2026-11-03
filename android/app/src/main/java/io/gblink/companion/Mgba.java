package io.gblink.companion;
final class Mgba {
    static { System.loadLibrary("companion"); }
    static native boolean load(byte[] rom, byte[] save, int type);
    static native int step(int keys, int[] pixels, short[] audio);
    static native byte[] save();
    static native int saveEvents();
    static native void close();
}
