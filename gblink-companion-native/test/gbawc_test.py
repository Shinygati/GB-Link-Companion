"""End-to-end test of Wonder Cards on your GBA (wcgba.go): the app (Linux build with -tags testhooks)
-> a fake GB-Link -> mGBA (b2s/test/mgba_wc) running the multiboot stub, then retail English
Emerald from the cartridge with your save. Uses the app's own buttons: start, switch the GBA on,
continue the game, use a card now, add one, Done.

  DISPLAY=:79 python3 gbawc_test.py APP EMERALD.gba SAVE.sav SHOTS

Needs: b2s/test/mgba_wc (see b2s/emerald_wc/README.md), Xvfb, ImageMagick. The save has to be one
that continues in the overworld."""
import json, os, shutil, socket, subprocess, sys, tempfile, time
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from fake_gblink import FakeGBLink
APP, ROM, SAVE, SHOTS = sys.argv[1:5]
MGBA_WC = os.environ.get('MGBA_WC', os.path.join(HERE, '../../b2s/test/mgba_wc'))
STUB = os.path.join(HERE, '../assets/emerald_wc_mb.gba')
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()


class Rig:
    """mgba_wc: link words on stdin/stdout, commands on a pipe (frames, buttons, memory reads)."""
    def __init__(self, save):
        r, w = os.pipe()
        self.log = os.path.join(tmp, 'mgba_wc.log')
        self.p = subprocess.Popen([MGBA_WC, STUB, ROM, save, '250'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=open(self.log, 'w'), pass_fds=(r,), env=dict(os.environ, RIG_CTL_FD=str(r)))
        os.close(r)
        self.ctl = os.fdopen(w, 'w')

    def run(self, c):
        before = open(self.log).read().count('done ' + c[0])
        self.ctl.write(c + '\n'); self.ctl.flush()
        while open(self.log).read().count('done ' + c[0]) == before:
            time.sleep(0.02)

    def frames(self, n): self.run('f%d' % n)

    def read(self, addr):
        self.run('r%x' % addr)
        return int([l for l in open(self.log).read().splitlines() if l.startswith('mem %08x' % addr)][-1].split('=')[1], 16)

    def press(self, keys, hold=6, after=20):
        self.run('k%x' % keys); self.frames(hold); self.run('k0'); self.frames(after)


class FakeWithEmerald(FakeGBLink):
    """After the multiboot, the GBA behind the fake adapter is mGBA running what was sent."""
    rig = None

    def _start_sim(self):
        save = os.path.join(tmp, 'emerald.sav'); shutil.copy(SAVE, save)
        self.rig = Rig(save)
        self.sim = self.rig.p
        self.state = 'running'; self.multiboots += 1

    def _stop_sim(self):
        if self.rig:
            self.rig.p.kill(); self.rig = self.sim = None


home = os.path.join(tmp, 'home'); os.makedirs(home)
json.dump({'setupDone': True, 'gamesFolder': tmp}, open(os.path.join(home, 'settings.json'), 'w'))
gba = FakeWithEmerald(os.path.join(tmp, 'cart.bin'))
env = dict(os.environ, GBLINK_PORT=gba.port, GBLINK_COMPANION_HOME=home, GBLINK_TEST_HOOKS='47971', LIBGL_ALWAYS_SOFTWARE='1')
app = subprocess.Popen([APP], env=env, stdout=open(os.path.join(tmp, 'app.log'), 'w'), stderr=subprocess.STDOUT, cwd=tmp)
import atexit
atexit.register(lambda: (app.kill(), gba._stop_sim()))
sock = None
for _ in range(300):
    try:
        sock = socket.create_connection(('127.0.0.1', 47971)); break
    except OSError:
        time.sleep(0.1)
rf = sock.makefile('r')
def cmd(c): sock.sendall((c + '\n').encode()); return rf.readline().strip()
def state(): return json.loads(cmd('state'))
def shot(n, d=0.8): time.sleep(d); subprocess.run(['import', '-window', 'root', '-crop', '920x740+0+0', f'{SHOTS}/{n}.png'])
def wait(cond, t, what):
    end = time.time() + t
    while time.time() < end:
        s = state()
        if cond(s): return s
        time.sleep(0.2)
    raise SystemExit('timed out: ' + what)

checks = {}
time.sleep(2); shot('1_home')
cmd('click gbawc')
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 20, 'switch-on prompt'); shot('2_switch_on')
gba.power_on_bios()
wait(lambda s: s['Phase'] == 'play', 60, 'multiboot'); shot('3_starting')
r = gba.rig
r.frames(300)                                            # the player continues their game
for i in range(60):
    if r.read(0x030022C4) == 0x0802F6B1: break         # the main menu
    r.press(8 if i % 2 == 0 else 1, 6, 50)
r.frames(60); r.press(1, 6, 300)
wait(lambda s: s['GBAWC'] == 'ready', 120, 'ready'); shot('4_ready')
checks['ready once walking around'] = True
cmd('click wc-open'); shot('5_dialog')
cmd('click wc-tick:BPEE0/031.wc3'); time.sleep(0.5)
cmd('click wc-use')
t0 = time.time()
while time.time() - t0 < 60:
    s = state()
    if s['WCStatus'].startswith('Used'): break
    if 'Waiting' in s['WCStatus']: r.press(1, 4, 10)  # the card's message box
    time.sleep(0.3)
checks['use now'] = state()['WCStatus'].startswith('Used')
shot('6_used')
cmd('click wc-tick:BPEE0/031.wc3'); cmd('click wc-tick:BPEE0/001.wc3'); time.sleep(0.5)
cmd('click wc-add')
try:
    wait(lambda s: s['WCStatus'].startswith('Received'), 30, 'add'); checks['add'] = True
except SystemExit:
    checks['add'] = False
shot('7_added')
sb1 = r.read(0x03005D8C)
checks['card in the save block'] = r.read(sb1 + 0x322C + 0x1C4) & 0xFFFF == 0x3E8   # Aurora Ticket's flag
cmd('click wc-close'); time.sleep(0.5)
cmd('click gbawc-done')
s = wait(lambda s: s['Phase'] == 'done', 20, 'done'); shot('8_done')
checks['done'] = s['Result'] == 'gbawc'
for k, v in checks.items():
    print(('PASS ' if v else 'FAIL ') + k)
sys.exit(0 if all(checks.values()) else 1)
