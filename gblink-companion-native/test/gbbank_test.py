"""The Game Boy Pokémon Bank with save files (as from a flash cart's SD card), through the native
app (Linux build with -tags testhooks) on a virtual display: a Crystal save's Pikachu is dragged
into the bank and saved, then dragged into a Red save (converted to generation 1, as the Time
Capsule does). Also screenshots the Home button, the Cartridge
tools page and the bank's choice page.
  DISPLAY=:79 python3 gbbank_test.py APP SHOTS"""
import json, os, shutil, socket, struct, subprocess, sys, tempfile, time
APP, SHOTS = sys.argv[1], sys.argv[2]
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()

def ch(t): return bytes(0x80 + ord(c) - ord('A') for c in t) + b'\x50'

def mon2(species, dv, moves, nick, exp=4096):
    b = bytearray(48)
    b[0] = species
    for i, m in enumerate(moves): b[2 + i] = m; b[0x17 + i] = 30
    struct.pack_into('>H', b, 6, 4321)
    b[8:11] = exp.to_bytes(3, 'big')
    struct.pack_into('>H', b, 0x15, dv)
    b[0x1B] = 70; b[0x1F] = 15
    return bytes(b), ch('ASH').ljust(11, b'\x50'), ch(nick).ljust(11, b'\x50')

def box_list(mons, body, party, slots=20, n=11):
    """a box list as the games keep it: count, species list, bodies, OT names, nicknames"""
    out = bytearray((n * 2 + body + 1) * slots + 2)
    out[0] = len(mons)
    at = 2 + slots
    for i, (d, ot, nick) in enumerate(mons):
        out[1 + i] = d[0]
        out[at + i * body:at + (i + 1) * body] = d[:body]
        out[at + slots * body + i * n:at + slots * body + (i + 1) * n] = ot[:n]
        out[at + slots * body + slots * n + i * n:at + slots * body + slots * n + (i + 1) * n] = nick[:n]
    out[1 + len(mons)] = 0xFF
    return bytes(out)

def crystal(mons):
    d = bytearray(0x8000)
    struct.pack_into('>H', d, 0x2009, 54321); d[0x200B:0x200B + 4] = ch('KRIS')
    d[0x4000:0x4000 + 0x44E] = box_list(mons, 32, 48)
    d[0x2D10:0x2D10 + 0x44E] = box_list(mons, 32, 48)           # box 1 is the current box
    d[0x2865], d[0x2866] = 0, 0xFF                              # an empty party list
    s = sum(d[0x2009:0x2B83]) & 0xFFFF
    struct.pack_into('<H', d, 0x2D0D, s); struct.pack_into('<H', d, 0x1F0D, s)
    return bytes(d)

def red():
    d = bytearray(0x8000)
    d[0x2598:0x2598 + 4] = ch('RED'); struct.pack_into('>H', d, 0x2605, 11111)
    d[0x30C0:0x30C0 + 0x462] = box_list([], 33, 44)
    d[0x2F2C], d[0x2F2D] = 0, 0xFF                              # an empty party list
    d[0x3523] = (~sum(d[0x2598:0x3523])) & 0xFF
    return bytes(d)

def gold_jp(mons):
    """a Japanese Gold save (PKHeX's SAV2Offsets, Japanese): 9 boxes of 30, names of 6"""
    d = bytearray(0x8000)
    struct.pack_into('>H', d, 0x2009, 2222); d[0x200B:0x200B + 3] = bytes([0x80, 0x81, 0x50])  # アイ
    d[0x283E], d[0x283F] = 0, 0xFF
    lst = box_list(mons, 32, 48, 30, 6)
    d[0x4000:0x4000 + len(lst)] = lst
    d[0x2D10:0x2D10 + len(lst)] = lst
    s = sum(d[0x2009:0x2C8C]) & 0xFFFF
    struct.pack_into('<H', d, 0x2D0D, s); struct.pack_into('<H', d, 0x7F0D, s)
    return bytes(d)

pika = mon2(25, 0xAAAA, [84, 45], 'PIKA')
jpmon = (mon2(152, 0x1111, [33, 45], 'A')[0], bytes([0x80, 0x81, 0x50, 0x50, 0x50, 0x50]), bytes([0xCF, 0x50, 0x50, 0x50, 0x50, 0x50]))
chiko = mon2(152, 0x1234, [33, 45], 'LEAF')
csav, rsav = os.path.join(tmp, 'Crystal.sav'), os.path.join(tmp, 'Red.sav')
open(csav, 'wb').write(crystal([pika, chiko]))
open(rsav, 'wb').write(red())

home = os.path.join(tmp, 'home'); os.makedirs(home)
games = os.path.join(tmp, 'games'); os.makedirs(games)
open(os.path.join(home, 'settings.json'), 'w').write(json.dumps({'mgba': '/bin/true', 'gamesFolder': games, 'setupDone': True}))
env = dict(os.environ, GBLINK_COMPANION_HOME=home, GBLINK_TEST_HOOKS='47962', LIBGL_ALWAYS_SOFTWARE='1', GBLINK_PORT='/dev/null-none')
def start(pick):
    env['GBLINK_TEST_PICK'] = pick
    return subprocess.Popen([APP], env=env, stdout=open(os.path.join(tmp, 'app.log'), 'a'), stderr=subprocess.STDOUT, cwd=tmp)
app = start(csav)
import atexit; atexit.register(lambda: app.poll() is None and app.kill())
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47962)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
def cmd(c): sock.sendall((c + '\n').encode()); return rf.readline().strip()
def state(): return json.loads(cmd('state'))
def gb():
    r = cmd('gbbank'); return json.loads(r) if r.startswith('{') else {}
def wait(cond, timeout=20, what='', get=state):
    end = time.time() + timeout
    while time.time() < end:
        s = get()
        if s and cond(s): return s
        time.sleep(0.2)
    raise SystemExit('timed out waiting for ' + what + '\n' + json.dumps(state())[:600] + '\n' + cmd('gbbank'))
def shot(name, delay=0.8):
    time.sleep(delay); subprocess.run(['import', '-window', 'root', f'{SHOTS}/{name}.png'])
checks = {}
wait(lambda s: s['Phase'] == 'home', 10, 'home')
shot('g0_home')
cmd('click tools-open'); shot('g1_tools')
cmd('click home-top'); time.sleep(0.5)
cmd('click bank-top'); shot('g2_bank_choice')
cmd('click gbbank')
wait(lambda b: b.get('Ready'), 10, 'bank open', gb); shot('g3_gbbank_empty')

# Crystal's save file: drag Pikachu into the bank, save
cmd('click gbbank-file')
b = wait(lambda b: b.get('Loaded'), 10, 'crystal loaded', gb)
checks['Crystal save opened'] = b['Game'] == 'Pokémon Crystal' and b['Cart'][:2] == [25, 152]
cmd('click gbbank-slot:c:0'); cmd('click gbbank-slot:b:0'); time.sleep(0.4)
checks['two clicks move nothing'] = gb()['ToBank'] == 0
cmd('gbbankdrop c:0 b:0')
b = wait(lambda b: b['ToBank'] == 1, 5, 'dragged', gb); shot('g4_dragged')
cmd('click gbbank-save')
b = wait(lambda b: b['Note'].startswith('Saved') and not b['Changed'], 20, 'saved', gb); shot('g5_saved')
pk2 = [os.path.join(dp, f) for dp, _, fs in os.walk(os.path.join(home, 'Pokemon Bank GB', 'Boxes')) for f in fs if f.endswith('.pk2')]
checks['saved in the bank as a .pk2'] = len(pk2) == 1 and os.path.getsize(pk2[0]) == 73
new = open(csav, 'rb').read()
checks['Crystal save written, Pikachu gone, checksums right'] = new[0x4000] == 1 and new[0x4001] == 152 and \
    struct.unpack_from('<H', new, 0x2D0D)[0] == sum(new[0x2009:0x2B83]) & 0xFFFF
checks['a copy of the file from before was kept'] = any(f.startswith('Crystal.sav.before-bank-') for f in os.listdir(tmp))
cmd('click gbbank-done'); wait(lambda s: s['Phase'] == 'done', 10, 'done')

# Red's save file: Pikachu comes over as a generation 1 Pokémon; Chikorita isn't allowed
app.kill(); app.wait(); sock.close()
app = start(rsav)
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47962)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
wait(lambda s: s['Phase'] == 'home', 10, 'home 2')
cmd('click gbbank'); wait(lambda b: b.get('Ready'), 10, 'bank open 2', gb)
cmd('click gbbank-file')
b = wait(lambda b: b.get('Loaded'), 10, 'red loaded', gb)
checks['Red save opened, bank kept Pikachu'] = b['Gen'] == 1 and b['Bank'][0] == 25 and b['Cart'][0] == 0
cmd('gbbankdrop b:0 c:0')
b = wait(lambda b: b['ToCart'] == 1, 5, 'to red', gb); shot('g6_to_red')
cmd('click gbbank-save')
b = wait(lambda b: b['Note'].startswith('Saved') and not b['Changed'], 20, 'saved red', gb)
r = open(rsav, 'rb').read()
checks['Red save has Pikachu in generation 1 form'] = r[0x30C0] == 1 and r[0x30C1] == 0x54 and r[0x30C0 + 22] == 0x54 and \
    r[0x3523] == (~sum(r[0x2598:0x3523])) & 0xFF
checks['the bank is empty again'] = gb()['InBank'] == 0
shot('g7_saved_red')

# A Japanese Gold save: its boxes hold 30, its Chikorita goes to the bank, and can't go to Red
cmd('click gbbank-done'); wait(lambda s: s['Phase'] == 'done', 10, 'done 2')
app.kill(); app.wait(); sock.close()
gsav = os.path.join(tmp, 'Gold JP.sav')
open(gsav, 'wb').write(gold_jp([jpmon]))
app = start(gsav)
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47962)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
wait(lambda s: s['Phase'] == 'home', 10, 'home 3')
cmd('click gbbank'); wait(lambda b: b.get('Ready'), 10, 'bank open 3', gb)
cmd('click gbbank-file')
b = wait(lambda b: b.get('Loaded'), 10, 'gold jp loaded', gb)
checks['Japanese Gold save opened'] = 'Japanese' in b['Game'] and b['Cart'][0] == 152 and len(b['Cart']) >= 30
shot('g8_japanese_gold')
cmd('gbbankdrop c:0 b:0')
b = wait(lambda b: b['ToBank'] == 1, 5, 'jp to bank', gb)
cmd('click gbbank-save')
b = wait(lambda b: b['Note'].startswith('Saved') and not b['Changed'], 20, 'saved jp', gb)
checks['Japanese Pokémon saved in the bank as a 63-byte .pk2'] = any(os.path.getsize(os.path.join(dp, f)) == 63
    for dp, _, fs in os.walk(os.path.join(home, 'Pokemon Bank GB', 'Boxes')) for f in fs if f.endswith('.pk2'))
cmd('click gbbank-done'); wait(lambda s: s['Phase'] == 'done', 10, 'done 3')
app.kill(); app.wait(); sock.close()
app = start(rsav)
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47962)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
wait(lambda s: s['Phase'] == 'home', 10, 'home 4')
cmd('click gbbank'); wait(lambda b: b.get('Ready'), 10, 'bank open 4', gb)
cmd('click gbbank-file')
b = wait(lambda b: b.get('Loaded'), 10, 'red loaded 2', gb)
cmd('gbbankdrop b:0 c:1'); time.sleep(0.5)
b = gb()
checks['a Japanese Pokémon can\'t go into an international Red'] = b['ToCart'] == 0 and 'Japanese' in b['Note']
shot('g9_refused')

for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
app.kill()
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
