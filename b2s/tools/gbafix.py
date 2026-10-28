#!/usr/bin/env python3
"""A stand-in for devkitPro's gbafix, enough for the multiboot images built here:
    gbafix.py <file.gba> [-p] [-tTITLE] [-cCODE] [-mMK]
Writes the Nintendo logo, the title, game code and maker code, and the header checksum.
(-p is accepted and ignored: multiboot images don't need padding.)"""
import sys

LOGO = bytes.fromhex(
    "24ffae51699aa2213d84820a84e409ad11248b98c0817f21a352be199309ce2010464a4af82731ec58c7e8"
    "3382e3cebf85f4df94ce4b09c194568ac01372a7fc9f844d73a3ca9a615897a327fc039876231dc7610304"
    "ae56bf38840040a70efdff52fe036f9530f197fbc08560d68025a963be03014e38e2f9a234ffbb3e0344"
    "780090cb88113a9465c07c6387f03cafd625e48b380aac7221d4f807")


def main(argv):
    path = argv[1]
    d = bytearray(open(path, "rb").read())
    d[4:0xA0] = LOGO
    for a in argv[2:]:
        if a.startswith("-t"):
            d[0xA0:0xAC] = a[2:14].encode("ascii").ljust(12, b"\0")
        elif a.startswith("-c"):
            d[0xAC:0xB0] = a[2:6].encode("ascii").ljust(4, b"\0")
        elif a.startswith("-m"):
            d[0xB0:0xB2] = a[2:4].encode("ascii").ljust(2, b"\0")
    d[0xB2] = 0x96
    d[0xBD] = (-(sum(d[0xA0:0xBD]) + 0x19)) & 0xFF
    open(path, "wb").write(d)


if __name__ == "__main__":
    main(sys.argv)
