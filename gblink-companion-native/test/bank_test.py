"""The Pokémon Bank through the native app (Linux build with -tags testhooks) on a virtual display:
fake GB-Link + simulated GBA running the real helper code, with a made-up Emerald cartridge (its
names, base stats and box icons where English Emerald keeps them) and a save with Pokémon in
box 1. Moves one to the bank and saves, brings it back, tries Move Multi, Release and a real
mouse drag, checks the bank's folders of PKHeX .pk3 files and their backups, and that a
different cartridge put in before saving is refused with nothing written.
  DISPLAY=:79 python3 bank_test.py APP SHOTS"""
import json, os, random, shutil, socket, struct, subprocess, sys, tempfile, time
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
APP, SHOTS = sys.argv[1], sys.argv[2]
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()
rnd = random.Random(5)

# ------------------------------------------------------------------ the games' text
def text(s, n):
    out = bytearray()
    for ch in s:
        if 'A' <= ch <= 'Z': out.append(0xBB + ord(ch) - 65)
        elif 'a' <= ch <= 'z': out.append(0xD5 + ord(ch) - 97)
        elif '0' <= ch <= '9': out.append(0xA1 + ord(ch) - 48)
        else: out.append(0x00)
    out.append(0xFF)
    return bytes(out[:n]).ljust(n, b'\x00')

# ------------------------------------------------------------------ a made-up Emerald ROM
ROM = bytearray(16 << 20)
ROM[4:8] = (0x51AEFF24).to_bytes(4, 'little')
ROM[0xA0:0xB0] = b'POKEMON EMERBPEE'
c = 0
for b in ROM[0xA0:0xBD]: c = (c - b) & 0xFF
ROM[0xBD] = (c - 0x19) & 0xFF
def at(a): return a - 0x08000000
for s in range(413):                                          # gSpeciesNames
    o = at(0x083185C8) + 11 * s
    ROM[o:o + 11] = text('MON%d' % s, 11)
for s in range(413):                                          # gSpeciesInfo: medium fast, half female
    o = at(0x083203CC) + 28 * s
    ROM[o + 16], ROM[o + 19], ROM[o + 22], ROM[o + 23] = 127, 0, 1 + s % 70, 0
for a in range(78):                                           # gAbilityNames
    o = at(0x0831B6DB) + 13 * a
    ROM[o:o + 13] = text('Ability%d' % a, 13)
for i in range(440):                                          # icons: a ball in colour i % 14 + 1
    p = 0x08900000 + 0x400 * i
    struct.pack_into('<I', ROM, at(0x0857BCA8) + 4 * i, p)
    ROM[at(0x0857C388) + i] = i % 6
    for y in range(32):
        for x in range(32):
            d = (x - 15.5) ** 2 + (y - 17.5) ** 2
            v = 0 if d > 13 ** 2 else 15 if d > 11 ** 2 else 1 + i % 14
            o = at(p) + ((y >> 3) * 4 + (x >> 3)) * 32 + (y & 7) * 4 + ((x & 7) >> 1)
            ROM[o] |= v << 4 if x & 1 else v
for k in range(6):                                            # the six icon palettes
    p = 0x08A00000 + 32 * k
    struct.pack_into('<IHH', ROM, at(0x0857C540) + 8 * k, p, 0xDAC0 + k, 0)
    for n in range(16):
        r, g, b = (n * 2 + k * 5) % 32, (31 - n * 2) % 32, (k * 6 + n) % 32
        struct.pack_into('<H', ROM, at(p) + 2 * n, r | g << 5 | b << 10)
    struct.pack_into('<H', ROM, at(p) + 30, 0)                # colour 15: the outline, black
mv = at(0x083185C8) + 11 * 413 + 37                           # move names, just after the species
ROM[mv:mv + 13] = text('-', 13)
for m in range(1, 355): ROM[mv + 13 * m:mv + 13 * m + 13] = text('Move%d' % m, 13)
for i in range(377):                                          # gItems: name, then the item's number
    o = at(0x085839A0) + 44 * i
    ROM[o:o + 14] = text('Item%d' % i, 14); struct.pack_into('<H', ROM, o + 14, i)
for m in range(0xD5):                                         # gRegionMapEntries
    o, p = at(0x085A147C) + 8 * m, 0x08B00000 + 24 * m
    struct.pack_into('<BBBBI', ROM, o, 1, 1, 1, 1, p)
    ROM[at(p):at(p) + 24] = text('Place%d' % m, 24)
rom_file = os.path.join(tmp, 'cart_rom.bin'); open(rom_file, 'wb').write(ROM)
os.environ.update(SIM_ROM_FILE=rom_file, FAKE_CHIP_MB='16')

# ------------------------------------------------------------------ Pokémon and the save
ORDER = [(0,1,2,3),(0,1,3,2),(0,2,1,3),(0,3,1,2),(0,2,3,1),(0,3,2,1),(1,0,2,3),(1,0,3,2),(2,0,1,3),(3,0,1,2),(2,0,3,1),(3,0,2,1),
         (1,2,0,3),(1,3,0,2),(2,1,0,3),(3,1,0,2),(2,3,0,1),(3,2,0,1),(1,2,3,0),(1,3,2,0),(2,1,3,0),(3,1,2,0),(2,3,1,0),(3,2,1,0)]
def pk3(pid, otid, species, exp):
    dec = bytearray(48)
    o = ORDER[pid % 24]
    struct.pack_into('<HHI', dec, 12 * o[0], species, 13, exp)                 # holding item 13
    struct.pack_into('<HHHHBBBB', dec, 12 * o[1], 33, 45, 0, 0, 35, 40, 0, 0)  # Move33, Move45
    struct.pack_into('<BBH', dec, 12 * o[3], 0, 16, 5 | 3 << 7 | 4 << 11)     # met at place 16, Lv 5, Emerald
    struct.pack_into('<I', dec, 12 * o[3] + 4, rnd.getrandbits(30))
    words = struct.unpack('<12I', dec)
    chk = sum((w & 0xFFFF) + (w >> 16) for w in words) & 0xFFFF
    enc = struct.pack('<12I', *[w ^ pid ^ otid for w in words])
    return struct.pack('<II', pid, otid) + text('NICK', 10) + bytes([2, 2]) + text('ASH', 7) + b'\x00' + struct.pack('<HH', chk, 0) + enc

SIZES = [0xF2C, 0xF80, 0xF80, 0xF80, 0xF08] + [0xF80] * 8 + [0x7D0]
def checksum(b):
    s = sum(struct.unpack('<%dI' % (len(b) // 4), b)) & 0xFFFFFFFF
    return ((s >> 16) + s) & 0xFFFF
def make_save(boxes, tid, sid, counter=10):
    raw = bytearray(0x20000)
    st = bytearray(8 * 0xF80 + 0x7D0)
    for (b, i), p in boxes.items(): st[4 + (b * 30 + i) * 80:4 + (b * 30 + i) * 80 + 80] = p
    for b in range(14): st[0x8344 + 9 * b:0x8344 + 9 * b + 9] = text('BOX %d' % (b + 1), 9)
    for slot, cnt in ((0, counter), (1, counter - 1)):
        for i in range(14):
            sid_ = (i + 5) % 14
            off = slot * 0xE000 + i * 0x1000
            sec = bytearray(0x1000)
            if sid_ >= 5: sec[:SIZES[sid_]] = st[(sid_ - 5) * 0xF80:(sid_ - 5) * 0xF80 + SIZES[sid_]]
            if sid_ == 0: sec[0:8] = text('BRENDAN', 8); struct.pack_into('<HH', sec, 0x0A, tid, sid)
            struct.pack_into('<HHII', sec, 0xFF4, sid_, checksum(bytes(sec[:SIZES[sid_]])), 0x08012025, cnt)
            raw[off:off + 0x1000] = sec
    return bytes(raw)
def boxes_of(raw):
    """the newest slot's box 1 and 2: species per slot (0 empty)"""
    best = None
    for slot in (0, 1):
        secs = {struct.unpack_from('<H', raw, slot * 0xE000 + i * 0x1000 + 0xFF4)[0]: slot * 0xE000 + i * 0x1000 for i in range(14)}
        cnt = struct.unpack_from('<I', raw, slot * 0xE000 + 0xFFC)[0]
        ok = all(checksum(raw[o:o + SIZES[i]]) == struct.unpack_from('<H', raw, o + 0xFF6)[0] for i, o in secs.items())
        if ok and (best is None or cnt > best[0]): best = (cnt, secs)
    st = b''.join(raw[best[1][i]:best[1][i] + (0xF80 if i < 13 else 0x7D0)] for i in range(5, 14))
    out = []
    for i in range(60):
        p = st[4 + 80 * i:84 + 80 * i]
        pid, otid = struct.unpack_from('<II', p)
        if pid == 0 and otid == 0: out.append(0); continue
        o = ORDER[pid % 24][0]
        w = struct.unpack_from('<I', p, 32 + 12 * o)[0] ^ pid ^ otid
        out.append(w & 0xFFFF)
    return best[0], out

TID, SID = 24680, 13579
OT = SID << 16 | TID
mons = {(0, 0): pk3(0x11111111, OT, 25, 8000), (0, 1): pk3(0x22222222, OT, 1, 125),
        (0, 2): pk3(0x00010001 ^ TID ^ SID, OT, 150, 1000000)}      # the last one shiny
cart = os.path.join(tmp, 'cart_save.bin'); open(cart, 'wb').write(make_save(mons, TID, SID))

from fake_gblink import FakeGBLink
gba = FakeGBLink(cart, 'flash128', log=lambda m: None)
home = os.path.join(tmp, 'home'); os.makedirs(home)
games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
open(os.path.join(home, 'settings.json'), 'w').write(json.dumps({'mgba': '/bin/true', 'controller': True, 'gamesFolder': games, 'setupDone': True}))
env = dict(os.environ, GBLINK_PORT=gba.port, GBLINK_COMPANION_HOME=home, GBLINK_TEST_HOOKS='47961',
           LIBGL_ALWAYS_SOFTWARE='1', GBLINK_MULTI='1')
app = subprocess.Popen([APP], env=env, stdout=open(os.path.join(tmp, 'app.log'), 'w'), stderr=subprocess.STDOUT, cwd=tmp)
import atexit; atexit.register(lambda: app.poll() is None and app.kill())
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47961)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
def cmd(c): sock.sendall((c + '\n').encode()); return rf.readline().strip()
def state(): return json.loads(cmd('state'))
def bank():
    r = cmd('bank')
    return json.loads(r) if r.startswith('{') else {}
def wait(cond, timeout=30, what='', get=state):
    end = time.time() + timeout
    while time.time() < end:
        s = get()
        if s and cond(s): return s
        time.sleep(0.2)
    raise SystemExit('timed out waiting for ' + what + '\n' + json.dumps(state())[:900] + '\n' + cmd('bank'))
def shot(name, delay=0.8):
    time.sleep(delay); subprocess.run(['import', '-window', 'root', f'{SHOTS}/{name}.png'])
BOXES = os.path.join(home, 'Pokemon Bank', 'Boxes')
def bank_file():
    """the .pk3 files in the bank's box folders"""
    if not os.path.isdir(BOXES): return []
    return sorted(os.path.join(d, f) for d in (os.path.join(BOXES, x) for x in os.listdir(BOXES)) for f in os.listdir(d) if f.endswith('.pk3'))
def mouse_drag(x0, y0, x1, y1, hold_shot=None):
    subprocess.run(['xdotool', 'mousemove', str(x0), str(y0)]); time.sleep(0.2)
    subprocess.run(['xdotool', 'mousedown', '1']); time.sleep(0.2)
    for k in range(1, 9):
        subprocess.run(['xdotool', 'mousemove', str(x0 + (x1 - x0) * k // 8), str(y0 + (y1 - y0) * k // 8)]); time.sleep(0.05)
    if hold_shot: shot(hold_shot, 0.5)
    subprocess.run(['xdotool', 'mouseup', '1']); time.sleep(0.4)
pending = os.path.join(home, 'Pokemon Bank', 'bank-pending.json')

checks = {}
time.sleep(2.5)
cmd('click bank')
wait(lambda s: s['Mode'] == 'bank', 10, 'bank opened')
b = wait(lambda b: b.get('Ready'), 10, 'bank open without a cartridge', bank); shot('k0_no_cartridge')
checks['the bank opens without a cartridge'] = not b['Loaded'] and gba.multiboots == 0
cmd('click bank-load')
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 20, 'switch on'); shot('k1_switch_on')
gba.power_on_bios()
b = wait(lambda b: b.get('Loaded'), 900, 'boxes read', bank)
shot('k2_boxes', 1.2)
checks['box 1 read from the cartridge'] = b['Cart']['Species'][:4] == [25, 1, 150, 0]
checks['names and icons read from the cartridge'] = b['Names'] and b['Icons']
checks['moves, items and places read from the cartridge'] = b['Moves'] == 355 and b['Items'] >= 300 and b['Places'] >= 200
checks['names and icons kept for next time'] = os.path.exists(os.path.join(home, 'Pokemon Bank', 'icons.bin')) and \
    os.path.exists(os.path.join(home, 'Pokemon Bank', 'names-BPEE.json'))

# pick up Pikachu (slot 1), put it in the bank: nothing changes until Save
cmd('click bank-slot:c:0'); time.sleep(0.3); shot('k3_picked_up')
cmd('click bank-slot:b:0'); b = wait(lambda b: b['ToBank'] == 1, 5, 'moved', bank); shot('k4_moved')
checks['moved to the bank (not saved yet)'] = b['Cart']['Species'][0] == 0 and b['Bank']['Species'][0] == 25 and bank_file() == []
cmd('click bank-slot:c:2'); time.sleep(0.3); shot('k4b_details_shiny')
checks['met place named as PKHeX does'] = bank()['SelPlace'] == 'Route 101'
time.sleep(0.3); cmd('click bank-slot:c:2'); time.sleep(0.3)  # put it back down where it was
cmd('click bank-save')
b = wait(lambda b: b['Note'].startswith('Saved'), 120, 'saved', bank); shot('k5_saved')
print('saved:', b['Note'])
files = bank_file()
checks['saved as a .pk3 in Box 01'] = len(files) == 1 and not os.path.exists(pending) and \
    os.path.basename(os.path.dirname(files[0])) == 'Box 01 - BANK 1' and os.path.basename(files[0]).startswith('Slot 01 - NICK - ')
if files:
    f = open(files[0], 'rb').read()
    checks['the .pk3 is decrypted as PKHeX saves it'] = len(f) == 80 and struct.unpack_from('<H', f, 0x20)[0] == 25
gba.power_off(); time.sleep(0.5)
cnt, sp = boxes_of(open(cart, 'rb').read())
checks['cartridge written as a new save (counter 11)'] = cnt == 11
checks['cartridge no longer has it'] = sp[:4] == [0, 1, 150, 0]
bdir = os.path.join(games, 'Save backups')
backups = os.listdir(bdir) if os.path.isdir(bdir) else []
checks['the save from before was backed up'] = len(backups) == 1 and 'before Pokemon Bank' in backups[0] and \
    boxes_of(open(os.path.join(bdir, backups[0]), 'rb').read())[1][:4] == [25, 1, 150, 0]

# bring it back to slot 6 of box 1: the GBA is off, so it asks
cmd('click bank-slot:b:0'); time.sleep(0.3); cmd('click bank-slot:c:5')
wait(lambda b: b['ToCart'] == 1, 5, 'moved back', bank)
cmd('click bank-save')
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 30, 'switch on to save'); shot('k6_switch_on_to_save')
gba.power_on_bios()
b = wait(lambda b: b['Note'].startswith('Saved') and not b['Changed'], 180, 'saved back', bank); shot('k7_saved_back')
checks['bank empty again'] = bank_file() == []
gba.power_off(); time.sleep(0.5)
cnt, sp = boxes_of(open(cart, 'rb').read())
checks['back on the cartridge in slot 6'] = sp[:6] == [0, 1, 150, 0, 0, 25] and cnt == 12
checks['each save backed up the bank before it'] = len(os.listdir(os.path.join(home, 'Pokemon Bank', 'Backups'))) == 1  # (the first save had nothing before it)

# Move Multi: choose two, send them to the bank; then undo
cmd('click bank-mode:multi'); time.sleep(0.3)
cmd('click bank-slot:c:1'); time.sleep(0.3); cmd('click bank-slot:c:2'); time.sleep(0.3)
b = wait(lambda b: b['Picked'] == 2, 5, 'two chosen', bank); shot('k9_multi_chosen')
cmd('click bank-to-bank')
b = wait(lambda b: b['ToBank'] == 2, 5, 'sent two', bank); shot('k10_multi_sent')
checks['Move Multi sends several to the bank'] = b['Bank']['Species'][:2] == [1, 150] and b['Cart']['Species'][1:3] == [0, 0] and b['Picked'] == 0
cmd('click bank-undo'); wait(lambda b: not b['Changed'], 5, 'undone', bank)

# Release: mark one, release it (asks first)
cmd('click bank-mode:release'); time.sleep(0.3)
cmd('click bank-slot:c:1'); time.sleep(0.3)
wait(lambda b: b['Picked'] == 1, 5, 'marked', bank); shot('k11_release_marked')
cmd('click bank-release')
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'question', 10, 'release asks'); shot('k12_release_ask')
cmd('answer release')
b = wait(lambda b: b['Released'] == 1, 5, 'released', bank)
checks['Release takes it out (until saved)'] = b['Cart']['Species'][1] == 0
cmd('click bank-undo'); wait(lambda b: not b['Changed'], 5, 'undone 2', bank)

# a real mouse drag, from the cartridge's slot 6 to the bank's slot 3
cmd('click bank-mode:move'); time.sleep(0.3)
subprocess.run(['xdotool', 'mousemove', '450', '420']); time.sleep(0.5)
b = bank()
ox, oy = 450 - b['At'][0], 420 - b['At'][1]                   # the boxes' place in the window
def centre(k):
    r = b['Rects'][k]; return int(ox + (r[0] + r[2]) / 2), int(oy + (r[1] + r[3]) / 2)
(x0, y0), (x1, y1) = centre('c:5'), centre('b:2')
print('drag from', (x0, y0), 'to', (x1, y1))
mouse_drag(x0, y0, x1, y1, 'k13_dragging')
b = wait(lambda b: b['ToBank'] == 1, 5, 'dragged', bank); shot('k14_dragged')
checks['a mouse drag moves it'] = b['Bank']['Species'][2] == 25 and b['Cart']['Species'][5] == 0
cmd('click bank-undo'); wait(lambda b: not b['Changed'], 5, 'undone 3', bank)

# Move Multi with a box: drag a box over slots 1-3 of box 1, then drag one of them to the bank
cmd('click bank-mode:multi'); time.sleep(0.4)
subprocess.run(['xdotool', 'mousemove', '450', '420']); time.sleep(0.5)
b = bank()
ox, oy = 450 - b['At'][0], 420 - b['At'][1]
r0, r2 = b['Rects']['c:0'], b['Rects']['c:2']
mouse_drag(int(ox + r0[0] + 3), int(oy + r0[1] + 3), int(ox + r2[2] - 3), int(oy + r2[3] - 3), 'k15_box_select')
b = wait(lambda b: b['Picked'] == 2, 5, 'box chose two', bank); shot('k16_box_chosen')
checks['a box chooses everything in it'] = b['Picked'] == 2
(x0, y0), (x1, y1) = centre('c:1'), centre('b:0')
mouse_drag(x0, y0, x1, y1, 'k17_group_drag')
b = wait(lambda b: b['ToBank'] == 2, 5, 'group dragged', bank); shot('k18_group_dropped')
checks['dragging a chosen one takes them all'] = b['Bank']['Species'][:2] == [1, 150]
checks['nothing is chosen after the drop'] = b['Picked'] == 0
cmd('click bank-undo'); wait(lambda b: not b['Changed'], 5, 'undone 4', bank)
cmd('click bank-mode:move'); time.sleep(0.3)

# a different cartridge before saving: refused, nothing written anywhere
cmd('click bank-slot:c:1'); time.sleep(0.3); cmd('click bank-slot:b:3')
wait(lambda b: b['ToBank'] == 1, 5, 'moved 3', bank)
other = make_save({(0, 0): pk3(0x33333333, 1, 7, 100)}, 1, 0, counter=50)
open(cart, 'wb').write(other)
cmd('click bank-save')
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 30, 'switch on 3')
gba.power_on_bios()
s = wait(lambda s: s['Phase'] == 'done', 120, 'refused'); shot('k8_other_cart')
print('refused:', s['ResultText'])
checks['another cartridge is refused'] = s['Result'] == 'failed' and 'different cartridge' in s['ResultText']
checks['the bank was left alone'] = bank_file() == [] and not os.path.exists(pending)
gba.power_off(); time.sleep(0.5)
checks['the other cartridge was left alone'] = open(cart, 'rb').read() == other

for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
print(open(os.path.join(tmp, 'app.log')).read()[-1500:])
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
