"""Builds a pretend Pokemon cart for testing gblink_controller.lua in real mGBA.

The ROM has the version's header code, and its tables (names, base stats, trainers, region map)
at the addresses from pret's symbol files; RAM (party, battle, map, RNG) is copied in at boot by
rom.s. Usage: mkrom.py KEY out.gba scenario    (scenario: wild | trainer | safari)
KEY: header code + revision (BPEE0, BPRE1, ...), with QoL on the end for Shinygati's QoL builds
"""
import json, os, re, struct, sys

KEY, OUT, SCEN = sys.argv[1], sys.argv[2], sys.argv[3]
HERE = os.path.dirname(os.path.abspath(__file__))
# pret's decompilations (pokeruby, pokeemerald, pokefirered) checked out side by side, for the
# region map data: set PRET to their folder
REF = os.environ.get('PRET', os.path.join(HERE, '../../../pret')) + '/'
DIRS = {'ru': 'pokeruby', 'em': 'pokeemerald', 'fr': 'pokefirered'}
syms = {}
for line in open(os.path.join(HERE, '../../assets/gblink_controller.lua')):
    m = re.match(r'add\("(\w+)", \{ game = "(\w+)", (.*) \}\)', line)
    if m:
        d = {'game': m.group(2)}
        for k, v in re.findall(r'(\w+) = 0x([0-9A-F]+)', m.group(3)):
            d[k] = int(v, 16)
        build = re.search(r'build = "(\w+)"', line)
        syms[m.group(1) + (build.group(1) if build else '')] = d
V = syms[KEY]
game = V['game']
frlg = game in ('FIRERED', 'LEAFGREEN')
rs = game in ('RUBY', 'SAPPHIRE')

rom = bytearray(b'\xff' * 0x1000000)
ewram = bytearray(0x40000)
iwram = bytearray(0x7000)


def put(addr, data):
    if 0x08000000 <= addr < 0x09000000:
        rom[addr - 0x08000000:addr - 0x08000000 + len(data)] = data
    elif 0x02000000 <= addr < 0x02040000:
        ewram[addr - 0x02000000:addr - 0x02000000 + len(data)] = data
    elif 0x03000000 <= addr < 0x03007000:
        iwram[addr - 0x03000000:addr - 0x03000000 + len(data)] = data
    else:
        raise ValueError(hex(addr))


CH = {' ': 0}
for i, c in enumerate('0123456789!?.-'):
    CH[c] = 0xA1 + i
for i, c in enumerate('ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'):
    CH[c] = 0xBB + i
CH["'"] = 0xB4
CH[','] = 0xB8
CH['/'] = 0xBA
CH[':'] = 0xF0
CH['&'] = 0x2D
CH['('], CH[')'] = 0x5C, 0x5D


def gs(s, size=None):
    s = re.sub(r'\{[A-Z_]+\}', '', s)
    b = bytes(CH.get(c, 0xAC) for c in s) + b'\xff'
    if size:
        b = b.ljust(size, b'\x00')
    return b


free = [0x08800000]          # spare ROM for strings that tables point to


def alloc(data):
    a = free[0]
    put(a, data)
    free[0] += (len(data) + 3) & ~3
    return a


# ---- species names / base stats / abilities / natures
SPECIES = {1: ('BULBASAUR', 45, 0x1F, 65, 0), 74: ('GEODUDE', 255, 0x3F, 69, 5),
           288: ('ZIGZAGOON', 255, 0x7F, 53, 54), 299: ('NOSEPASS', 255, 0x7F, 5, 42),
           286: ('POOCHYENA', 255, 0x7F, 22, 0), 19: ('RATTATA', 255, 0x7F, 50, 62)}
for s, (name, rate, gender, ab1, ab2) in SPECIES.items():
    put(V['speciesNames'] + 11 * s, gs(name, 11))
    info = bytearray(28)
    info[8], info[16], info[22], info[23] = rate, gender, ab1, ab2
    put(V['speciesInfo'] + 28 * s, bytes(info))
ABIL = {65: 'OVERGROW', 69: 'ROCK HEAD', 5: 'STURDY', 53: 'PICKUP', 54: 'TRUANT', 42: 'MAGNET PULL',
        22: 'INTIMIDATE', 62: 'GUTS', 0: '-------'}
for a, n in ABIL.items():
    put(V['abilityNames'] + 13 * a, gs(n, 13))
NAT = ['HARDY', 'LONELY', 'BRAVE', 'ADAMANT', 'NAUGHTY', 'BOLD', 'DOCILE', 'RELAXED', 'IMPISH', 'LAX',
       'TIMID', 'HASTY', 'SERIOUS', 'JOLLY', 'NAIVE', 'MODEST', 'MILD', 'QUIET', 'BASHFUL', 'RASH',
       'CALM', 'GENTLE', 'SASSY', 'CAREFUL', 'QUIRKY']
for i, n in enumerate(NAT):
    put(V['natureNames'] + 4 * i, struct.pack('<I', alloc(gs(n))))

# ---- pictures: made-up test art (a ringed ball), not anything from the games
def lz_literal(data):                      # GBA LZ77 with only literal blocks
    out = bytearray(struct.pack('<I', 0x10 | len(data) << 8))
    for i in range(0, len(data), 8):
        out.append(0)
        out += data[i:i + 8]
    return bytes(out)


def tiles(size, colour_at):                # 4bpp tiles, row-major, low nibble = left pixel
    out = bytearray()
    for ty in range(size // 8):
        for tx in range(size // 8):
            for y in range(8):
                for x in range(0, 8, 2):
                    a = colour_at(tx * 8 + x, ty * 8 + y)
                    b = colour_at(tx * 8 + x + 1, ty * 8 + y)
                    out.append(a | b << 4)
    return bytes(out)


def ball(size):
    c = size / 2
    def at(x, y):
        d = ((x + 0.5 - c) ** 2 + (y + 0.5 - c) ** 2) ** 0.5
        if d > c - 1: return 0
        if d > c - 3: return 1
        return 2 if y < c else 3
    return at


def pal(*rgb):
    return b''.join(struct.pack('<H', r | g << 5 | b << 10) for r, g, b in rgb).ljust(32, b'\0')


front = alloc(lz_literal(tiles(64, ball(64))))
normal = alloc(lz_literal(pal((0, 0, 0), (4, 4, 4), (31, 10, 8), (28, 28, 28))))
shiny = alloc(lz_literal(pal((0, 0, 0), (4, 4, 4), (31, 26, 4), (28, 28, 28))))
icon = alloc(tiles(32, ball(32)))
put(V['iconPals'], struct.pack('<II', alloc(pal((0, 0, 0), (4, 4, 4), (8, 20, 31), (28, 28, 28))), 0))
for s in SPECIES:
    put(V['frontPics'] + 8 * s, struct.pack('<IHH', front, 2048, s))
    put(V['palettes'] + 8 * s, struct.pack('<IHH', normal, s, 0))
    put(V['shinyPalettes'] + 8 * s, struct.pack('<IHH', shiny, s, 0))
    put(V['icons'] + 4 * s, struct.pack('<I', icon))
    put(V['iconPalIdx'] + s, b'\0')

# ---- trainer
put(V['classNames'] + 13 * 32, gs('LEADER', 13))
t = bytearray(40)
t[1] = 32
t[4:16] = gs('ROXANNE', 12)
put(V['trainers'] + 40 * 265, bytes(t))

# ---- region map
SECS = json.load(open(REF + DIRS[{'RUBY': 'ru', 'SAPPHIRE': 'ru', 'EMERALD': 'em'}.get(game, 'fr')] +
                      '/src/data/region_map/region_map_sections.json'))['map_sections']
IDX = {s['id']: i for i, s in enumerate(SECS)}
IDX.setdefault('MAPSEC_NONE', 0xC5 if game in ('FIRERED', 'LEAFGREEN') else len(SECS))


def secname(s):
    n = s.get('name', '')
    if '{NAME_END}' in n:            # Ruby/Sapphire: "LITTLEROOT" <end> " TOWN"
        a, b = n.split('{NAME_END}')
        return gs(a) + gs(b)
    return gs(n.replace('{PKMN}', 'PKMN'))


if frlg:
    for i in range(0x58, 0x58 + 109):
        if i < len(SECS):
            put(V['mapNames'] + 4 * (i - 0x58), struct.pack('<I', alloc(secname(SECS[i]))))
    NONE = IDX['MAPSEC_NONE']
    for key, fn in (('layoutKanto', 'kanto'), ('layoutSevii123', 'sevii_123'), ('layoutSevii45', 'sevii_45'), ('layoutSevii67', 'sevii_67')):
        txt = open(REF + f'pokefirered/src/data/region_map/region_map_layout_{fn}.h').read()
        ids = re.findall(r'MAPSEC_\w+', txt.split('=', 1)[1])
        assert len(ids) == 2 * 15 * 22, (fn, len(ids))
        put(V[key], bytes(IDX[i] for i in ids))
    sev = bytearray([NONE] * 90)
    src = open(REF + 'pokefirered/src/region_map.c').read()
    body = src[src.index('sSeviiMapsecs[3][30]'):]
    groups = re.findall(r'\{([^{}]*)\}', body[:body.index('};')])
    for g, grp in enumerate(groups[:3]):
        for j, m in enumerate(re.findall(r'MAPSEC_\w+', grp)):
            sev[30 * g + j] = IDX[m]
    put(V['seviiMapsecs'], bytes(sev))
else:
    for i, s in enumerate(SECS):
        if i >= 0x57 and game != 'EMERALD':
            break
        e = struct.pack('<BBBBI', s.get('x', 0), s.get('y', 0), s.get('width', 0), s.get('height', 0), alloc(secname(s)))
        put(V['mapEntries'] + 8 * i, e)

# ---- a Pokemon (party struct, 100 bytes), encrypted like the games do
ORDER = [(0,1,2,3),(0,1,3,2),(0,2,1,3),(0,3,1,2),(0,2,3,1),(0,3,2,1),(1,0,2,3),(1,0,3,2),(2,0,1,3),(3,0,1,2),(2,0,3,1),(3,0,2,1),
         (1,2,0,3),(1,3,0,2),(2,1,0,3),(3,1,0,2),(2,3,0,1),(3,2,0,1),(1,2,3,0),(1,3,2,0),(2,1,3,0),(3,1,2,0),(2,3,1,0),(3,2,1,0)]


def mon(species, pid, otid, level, hp, maxhp, ivs, ability_bit=0, status=0):
    subs = [bytearray(12) for _ in range(4)]
    struct.pack_into('<HH', subs[0], 0, species, 0)
    hpv, atk, de, spa, spd, spe = ivs
    w = hpv | atk << 5 | de << 10 | spe << 15 | spa << 20 | spd << 25 | ability_bit << 31
    struct.pack_into('<I', subs[3], 4, w)
    o = ORDER[pid % 24]
    plain = bytearray(48)
    for typ in range(4):
        plain[12 * o[typ]:12 * o[typ] + 12] = subs[typ]
    ck = sum(struct.unpack('<24H', plain)) & 0xFFFF
    key = pid ^ otid
    enc = b''.join(struct.pack('<I', x ^ key) for x in struct.unpack('<12I', plain))
    m = bytearray(100)
    struct.pack_into('<II', m, 0, pid, otid)
    struct.pack_into('<H', m, 28, ck)
    m[32:80] = enc
    struct.pack_into('<IBBHH', m, 80, status, level, 0, hp, maxhp)
    return bytes(m)


# a shiny Zigzagoon: TID ^ SID ^ PIDhi ^ PIDlo < 8
OTID = 0x1A2B3C4D
shinypid = ((0x1A2B ^ 0x3C4D ^ 0x0007) << 16) | 0x0007
shinypid ^= 0  # nature = pid % 25
ZIG = mon(288, shinypid, OTID, 3, 11, 14, (31, 12, 5, 20, 30, 18), 0)

inbattle = 0x43D if rs else 0x439
main = V['main']
put(V['rng'], struct.pack('<I', 0x5A0C))          # seeded with a 16-bit value, advances once per frame
put(0x08000400, struct.pack('<I', V['rng']))
put(main + 0x24, struct.pack('<I', 12345))

if SCEN == 'wild':
    put(V['enemy'], ZIG)
    put(main + inbattle, b'\x02')
    put(V['battleType'], struct.pack('<I', 0))
    bm = bytearray(0x58)
    struct.pack_into('<H', bm, 0, 288)
    struct.pack_into('<H', bm, 0x28, 3)        # HP 3/14, asleep
    struct.pack_into('<H', bm, 0x2C, 14)
    struct.pack_into('<I', bm, 0x4C, 2)
    put(V['battleMons'] + 0x58, bytes(bm))
elif SCEN == 'safari':
    # this one was made by the RNG on frame 3 after seed 5A0C (PID: low half, then high half)
    A_, C_ = 0x41C64E6D, 0x6073
    x = 0x5A0C
    for _ in range(3):
        x = (A_ * x + C_) & 0xFFFFFFFF
    r1 = (A_ * x + C_) & 0xFFFFFFFF
    r2 = (A_ * r1 + C_) & 0xFFFFFFFF
    put(V['enemy'], mon(288, (r2 >> 16) << 16 | r1 >> 16, OTID, 3, 14, 14, (1, 2, 3, 4, 5, 6)))
    put(main + inbattle, b'\x02')
    put(V['battleType'], struct.pack('<I', 0x80))
    bs = 0x02016086 if rs else 0x02030000 + 0x79
    if not rs:
        put(V['battleStructPtr'], struct.pack('<I', 0x02030000))
    put(bs, bytes([1, 2, 6, 8]))   # counters 1 and 2, escape factor 6, catch factor 8
elif SCEN == 'trainer':
    put(V['enemy'], mon(74, 0x12345678, 0x0BADF00D, 12, 30, 30, (31, 31, 31, 31, 31, 31)))
    put(V['enemy'] + 100, mon(299, 0x87654321, 0x0BADF00D, 15, 0, 41, (1, 2, 3, 4, 5, 6), 1))
    put(main + inbattle, b'\x02')
    put(V['battleType'], struct.pack('<I', 8))
    put(V['opponent'], struct.pack('<H', 265))
    put(V['partyIdx'], struct.pack('<4H', 0, 0, 1, 1))

# map: Route 104 for Hoenn (a tall route), Route 4 / Four Island for Kanto
if frlg:
    mapsec = IDX['MAPSEC_FOUR_ISLAND'] if SCEN == 'trainer' else IDX['MAPSEC_ROUTE_4']
else:
    mapsec = IDX['MAPSEC_ROUTE_104']
layout = alloc(struct.pack('<ii', 40, 80))
mh = bytearray(0x1C)
struct.pack_into('<I', mh, 0, layout)
mh[0x14], mh[0x17] = mapsec, 3
put(V['mapHeader'], bytes(mh))
sb1 = V.get('sb1', 0x02025A00)
if 'sb1ptr' in V:
    put(V['sb1ptr'], struct.pack('<I', sb1))
put(sb1, struct.pack('<hhbb', 30, 70, 0, 19))
sb2 = V.get('sb2', 0x02026000)               # the player: TID 12345, SID 54321
if 'sb2ptr' in V:
    put(V['sb2ptr'], struct.pack('<I', sb2))
put(sb2 + 0x0A, struct.pack('<HH', 12345, 54321))

# ---- the place's map: a route with grass, water and a path, 3 trainers and 2 hidden items
FAM = {'RUBY': (0x1220, 0x258, 512), 'SAPPHIRE': (0x1220, 0x258, 512), 'EMERALD': (0x1270, 0x1F4, 512),
       'FIRERED': (0xEE0, 1000, 640), 'LEAFGREEN': (0xEE0, 1000, 640)}[game]
flags_off, hidden_start, nprim = FAM


def solid(c):                                  # an 8x8 tile of one colour
    return bytes([c | c << 4]) * 32


ptiles = alloc(solid(0) + solid(1) + solid(2) + solid(3))
tpal = bytearray(16 * 32)
tpal[0:8] = struct.pack('<4H', 0, 6 << 5 | 2, 6 | 12 << 5 | 26 << 10, 22 | 16 << 5 | 8)  # -, grass, water, path
ppals = alloc(bytes(tpal))
meta = b''.join(struct.pack('<8H', *([t] * 4 + [0] * 4)) for t in (1, 2, 3))   # grass, water, path
pmeta = alloc(meta)
primary = alloc(struct.pack('<BBHIIIII', 0, 0, 0, ptiles, ppals, pmeta, 0, 0))
secondary = alloc(struct.pack('<BBHIIIII', 0, 1, 0, ptiles, ppals, pmeta, 0, 0))
W, H = 40, 80
blocks = bytearray()
for y in range(H):
    for x in range(W):
        blocks += struct.pack('<H', 1 if x < 8 else 2 if x in (20, 21) else 0)
route_layout = alloc(struct.pack('<iiIIII', W, H, 0, alloc(bytes(blocks)), primary, secondary))


def trainer_script(tid):
    return alloc(bytes([0x5C, 0]) + struct.pack('<HH', tid, 0) + b'\x02')


objs = b''.join(struct.pack('<BBBxhhBBHHHIH2x', i + 1, 1, 0, 5, 5 + i, 3, 0, 0, 1, 3, trainer_script(tid), 0)
                for i, tid in enumerate((100, 101, 102)))
def hidden(flag_id):
    if game in ('FIRERED', 'LEAFGREEN'):
        return struct.pack('<HHBBxxI', 3, 3, 0, 7, 0x0D | flag_id << 16 | 1 << 24)
    return struct.pack('<HHBBxxHH', 3, 3, 0, 7, 0x0D, flag_id)
bgs = hidden(5) + hidden(6)
events = alloc(struct.pack('<BBBBIIII', 3, 0, 0, 2, alloc(objs), 0, 0, alloc(bgs)))
route = bytearray(0x1C)
struct.pack_into('<II', route, 0, route_layout, events)
route[0x14], route[0x17] = mapsec, 3
route_hdr = alloc(bytes(route))
group = V['mapGroups'] - 0x40                   # the group's list comes before gMapGroups
put(group, struct.pack('<I', route_hdr))
put(V['mapGroups'], struct.pack('<II', group, 0))
# beaten: trainer 101; found: the first hidden item
def setflag(f):
    a = sb1 + flags_off + (f >> 3)
    ewram[a - 0x02000000] |= 1 << (f & 7)
setflag(0x500 + 101)
setflag(hidden_start + 5)

# ---- the region map's graphics (made-up test art: sea with a green island and a frame)
def lz_any(data):
    return lz_literal(bytes(data))
if frlg:
    t4 = bytearray()
    for c in (1, 2, 3):                        # tiles: sea, land, frame; 4bpp
        t4 += bytes([c | c << 4]) * 32
    tmap = bytearray()
    for y in range(20):
        for x in range(30):
            t = 2 if y < 4 or x < 2 or x > 27 else (1 if 8 <= x <= 20 and 6 <= y <= 15 else 0)
            tmap += struct.pack('<H', t)
    for key in ('rmKanto', 'rmSevii123', 'rmSevii45', 'rmSevii67'):
        put(V[key], lz_any(tmap))
    put(V['rmTiles'], lz_any(t4)); put(V['rmPal'], pal((2, 4, 10), (6, 12, 28), (8, 24, 6), (28, 26, 10)))
else:
    t8 = bytearray()
    for c in (0x71, 0x72, 0x73):               # tiles: sea, land, frame; 8bpp, colours from 0x70
        t8 += bytes([c]) * 64
    tmap = bytearray()
    for y in range(64):
        for x in range(64):
            tmap.append(2 if y < 2 or x < 1 or x > 28 else (1 if 6 <= x <= 22 and 5 <= y <= 14 else 0))
    put(V['rmTiles'], lz_any(t8)); put(V['rmTilemap'], lz_any(tmap))
    put(V['rmPal'], pal((2, 4, 10), (6, 12, 28), (8, 24, 6), (28, 26, 10)))
hero = tiles(16, lambda x, y: 0 if (x - 7.5) ** 2 + (y - 7.5) ** 2 > 36 else (3 if y < 8 else 1))
cursor = tiles(16, lambda x, y: 2 if x in (0, 15) or y in (0, 15) else 0) + bytes(128)
for k in ('rmHero', 'rmHeroine'):
    put(V[k], lz_any(hero) if frlg else hero)
    put(V[k + 'Pal'], pal((0, 0, 0), (31, 31, 31), (31, 28, 4), (31, 4, 4)))
put(V['rmCursor'], lz_any(cursor)); put(V['rmCursorPal'], pal((0, 0, 0), (31, 31, 31), (31, 28, 4)))
# (made-up icons are bigger than the games' compressed ones and can run into the next table: write
# the region map's tiles again last)
if frlg:
    put(V['rmTiles'], lz_any(t4))
else:
    put(V['rmTiles'], lz_any(t8))

# ---- header and program
prog = open(os.path.join(HERE, 'rom.bin'), 'rb').read()   # built from rom.s
rom[0:len(prog)] = prog
logo = open(os.path.join(HERE, '../../assets/gblink_sync_mb.gba'), 'rb').read()[4:0xA0]
rom[4:0xA0] = logo
rom[0xA0:0xAC] = b'POKEMON TEST'
rom[0x500:0x50C] = b'FLASH1M_V103'      # the save library tag: mGBA gives the cart a 128 KB flash chip
code = KEY[:4].encode()   # BPEE0QoL -> BPEE, revision 0
rom[0xAC:0xB0] = code
rom[0xB0:0xB2] = b'01'
rom[0xB2] = 0x96
rom[0xBC] = int(KEY[4])
rom[0xBD] = (-(sum(rom[0xA0:0xBD]) + 0x19)) & 0xFF
rom[0x100000:0x140000] = ewram
rom[0x140000:0x147000] = iwram
open(OUT, 'wb').write(rom)
print('built', OUT, KEY, SCEN, 'shiny pid', hex(shinypid))
