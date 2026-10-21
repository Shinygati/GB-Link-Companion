"""Backing up a cartridge through the native app (Linux build with -tags testhooks) on a virtual
display: fake GB-Link + simulated GBA running the real helper code, with a 1 MB game and a
save in its cart. Checks the .gba and .sav it writes, and that Cancel stops a backup cleanly.
  DISPLAY=:79 python3 backup_test.py APP SHOTS"""
import json, os, random, shutil, socket, subprocess, sys, tempfile, time
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
APP, SHOTS = sys.argv[1], sys.argv[2]
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()
rnd = random.Random(11)
rom = bytearray(rnd.getrandbits(8) for _ in range(1 << 20))
rom[4:8] = (0x51AEFF24).to_bytes(4, 'little')                       # start of the Nintendo logo
rom[0xA0:0xB0] = b'POKEMON EMERBPEE'
c = 0
for b in rom[0xA0:0xBD]: c = (c - b) & 0xFF
rom[0xBD] = (c - 0x19) & 0xFF                                         # a valid header checksum
rom_file = os.path.join(tmp, 'cart_rom.bin'); open(rom_file, 'wb').write(rom)
os.environ['SIM_ROM_FILE'] = rom_file
from fake_gblink import FakeGBLink
games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
cart = os.path.join(tmp, 'cart_save.bin')
save = bytes(rnd.getrandbits(8) for _ in range(0x20000)); open(cart, 'wb').write(save)
gba = FakeGBLink(cart, 'flash128', log=lambda m: None)
home = os.path.join(tmp, 'home'); os.makedirs(home)
open(os.path.join(home, 'settings.json'), 'w').write(json.dumps({'mgba': '/bin/true', 'controller': True, 'gamesFolder': games, 'setupDone': True}))
env = dict(os.environ, GBLINK_PORT=gba.port, GBLINK_COMPANION_HOME=home, GBLINK_TEST_HOOKS='47960',
           LIBGL_ALWAYS_SOFTWARE='1', GBLINK_MULTI='1')
app = subprocess.Popen([APP], env=env, stdout=open(os.path.join(tmp, 'app.log'), 'w'), stderr=subprocess.STDOUT, cwd=tmp)
import atexit; atexit.register(lambda: app.poll() is None and app.kill())
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47960)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
def cmd(c): sock.sendall((c + '\n').encode()); return rf.readline().strip()
def state(): return json.loads(cmd('state'))
def wait(cond, timeout=30, what=''):
    end = time.time() + timeout
    while time.time() < end:
        s = state()
        if cond(s): return s
        time.sleep(0.15)
    raise SystemExit('timed out waiting for ' + what + '\n' + json.dumps(state())[:800])
def shot(name, delay=0.6):
    time.sleep(delay); subprocess.run(['import', '-window', 'root', '-crop', '920x740+0+0', f'{SHOTS}/{name}.png'])
def mouse(x, y):
    subprocess.run(['xdotool', 'mousemove', str(x), str(y)]); time.sleep(0.15); subprocess.run(['xdotool', 'click', '1'])

checks = {}
time.sleep(2.5)
shot('b1_games_empty')
cmd('click backup')                                # "Back up a cartridge"
wait(lambda s: s['Mode'] == 'backup', 10, 'backup started')
checks['Back up button (real click)'] = True
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 20, 'switch on'); shot('b2_switch_on')
gba.power_on_bios()
s = wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'question', 30, 'confirm'); shot('b3_confirm')
checks['found the game'] = s['Game']['Code'] == 'BPEE' and '1 MB' in s['Prompt']['Text']
cmd('answer go')
wait(lambda s: s['Progress'] and s['Progress']['Done'] > 0, 20, 'copying'); shot('b4_copying', 1.5)
s = wait(lambda s: s['Phase'] == 'done', 300, 'done'); shot('b5_done')
dest = s['BackupPath']
print('saved to', dest, '|', s['ResultText'])
checks['backed up'] = s['Result'] == 'backup'
checks['file name from the header'] = os.path.basename(dest) == 'POKEMON EMER (BPEE).gba'
checks['ROM matches the cart'] = open(dest, 'rb').read() == bytes(rom)
checks['save copied next to it'] = open(dest[:-4] + '.sav', 'rb').read()[:0x20000] == save
cmd('click details'); time.sleep(0.5); cmd('click copy-log'); shot('b5b_details_copied', 0.8)
try:
    clip = subprocess.run(['xclip', '-o', '-selection', 'clipboard'], capture_output=True, text=True, timeout=5).stdout
    checks['Copy log puts the log on the clipboard'] = 'Cartridge: POKEMON EMER [BPEE]' in clip
except FileNotFoundError:
    pass
cmd('click home'); s = wait(lambda s: s['Phase'] == 'home', 10, 'home'); shot('b6_games_after')
checks['listed in your games'] = any(g['ROM'] == dest for g in s['Library'])

# Cancel half-way: nothing is left behind
cmd('click backup')
s = wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'question', 30, 'confirm 2')
cmd('answer go')
wait(lambda s: s['Progress'] and s['Progress']['Done'] > 200000, 120, 'copying 2')
cmd('click cancel-copy')
s = wait(lambda s: s['Phase'] == 'done', 60, 'stopped'); shot('b7_cancelled')
checks['Cancel stops it'] = s['Result'] == 'cancelled'
checks['no partial file left'] = not [f for f in os.listdir(games) if f.endswith('.part') or '(2)' in f]
gba.power_off()
for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
print(open(os.path.join(tmp, 'app.log')).read()[-1200:])
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
