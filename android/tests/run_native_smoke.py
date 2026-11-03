"""Run the exact mGBA JNI frontend against an original, generated test cartridge.

First build app/src/main/cpp/CMakeLists.txt for your host, with JAVA_HOME set.
This is independent of the Android ABI build and does not emulate Android.
"""
import argparse
from pathlib import Path
import subprocess
import tempfile

p = argparse.ArgumentParser()
p.add_argument("--library-dir", required=True)
p.add_argument("--prefix", default="arm-none-eabi-")
args = p.parse_args()
root = Path(__file__).resolve().parents[1]
src = root / "app/src/main/java/io/gblink/companion"

def run(*command):
    subprocess.run([str(x) for x in command], check=True)

with tempfile.TemporaryDirectory(prefix="gblink-native-smoke-") as name:
    d = Path(name)
    run(args.prefix + "as", "-mcpu=arm7tdmi", root / "tests/native-smoke.s", "-o", d / "test.o")
    run(args.prefix + "ld", "-Ttext=0x08000000", d / "test.o", "-o", d / "test.elf")
    run(args.prefix + "objcopy", "-O", "binary", d / "test.elf", d / "test.gba")
    rom = bytearray((d / "test.gba").read_bytes())
    rom[0xA0:0xAC] = b"GBLINK TEST "
    rom[0xAC:0xB0] = b"TEST"
    rom[0xB2] = 0x96
    rom[0xBD] = (-sum(rom[0xA0:0xBD]) - 0x19) & 255
    (d / "test.gba").write_bytes(rom)
    run("javac", "--release", "8", "-d", d,
        *[src / (n + ".java") for n in ("Wire", "Store", "Mgba")],
        root / "tests/NativeSmoke.java")
    run("java", "--enable-native-access=ALL-UNNAMED",
        "-Djava.library.path=" + str(Path(args.library_dir).resolve()),
        "-cp", d, "io.gblink.companion.NativeSmoke", d / "test.gba")
