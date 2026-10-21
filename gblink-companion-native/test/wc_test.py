"""Wonder Cards end to end (wc_test.py): the app sends cards to Pokemon Emerald running in "mGBA"
(libmgba + the real script), and the game's own code takes them. Needs a verified Emerald ROM.
  DISPLAY=:79 python3 wc_test.py APP ROM
(the rest as the status screens' test:, with real code everywhere it matters:
  - the app (Linux build with -tags testhooks) on a virtual display,
  - a fake GB-Link whose GBA is mGBA running the real helper (b2s/test/mgba_link), with a test
    Pokemon cart in its slot - its screen is saved as screenshots,
  - "mGBA" on the PC is libmgba running the same test cart with the app's real Lua script
    (status/runlua), so the script reads the game exactly as it would in mGBA.
The test cart (status/mkrom.py) is a pretend Pokemon game: its tables and RAM are where pret's
symbol files say the real game keeps them.
  DISPLAY=:79 python3 status_test.py APP SHOTS KEY SCENARIO
  e.g. KEY = BPEE0 (Emerald), BPRE1 (FireRed 1.1), AXVE0 (Ruby); SCENARIO = wild | trainer | safari
Needs: mgba_link and runlua built against libmgba (see status/README.md), Xvfb, ImageMagick."""
import json, os, shutil, socket, subprocess, sys, tempfile, time
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from fake_gblink import FakeGBLink


APP, SRC_ROM = sys.argv[1:3]
MGBA_LINK = os.environ.get('MGBA_LINK', os.path.join(HERE, '../../b2s/test/mgba_link'))
RUNLUA = os.environ.get('RUNLUA', os.path.join(HERE, 'status/runlua'))
HELPER = os.path.join(HERE, '../assets/gblink_sync_mb.gba')
tmp = tempfile.mkdtemp()
games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
rom = os.path.join(games, 'Pokemon Emerald.gba'); shutil.copy(SRC_ROM, rom)
cart_rom = os.path.join(tmp, 'cart.gba'); shutil.copy(SRC_ROM, cart_rom)
index = json.load(open(os.path.join(HERE, '../assets/wondercards/index.json')))
def card(label): return next(c['file'] for c in index if c['game'] == 'BPEE0' and c['label'] == label)
class MGBALink(FakeGBLink):
    """The GBA behind the fake adapter is mGBA running the real helper binary."""
    def _start_sim(self):
        r, w = os.pipe()
        self.ctl_w = w
        def child():
            os.dup2(r, 3)
        env = dict(os.environ, MGBA_CART=cart_rom)
        self.sim = subprocess.Popen([MGBA_LINK, HELPER, '60'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                    stderr=open(os.path.join(tmp, 'mgba_link.log'), 'a'), env=env,
                                    preexec_fn=child, pass_fds=(3,))
        os.close(r)
        self.sim_dump = None
        self.state = 'running'
        self.multiboots += 1
        self.log('helper started in mGBA')

    def _stop_sim(self):
        if self.sim:
            self.sim.stdin.close(); self.sim.wait(); self.sim = None

    def ctl(self, s):
        os.write(self.ctl_w, (s + '\n').encode()); time.sleep(0.05)

    def keys(self, k):
        self.ctl('k%x' % k)

    def shot_unused(self, name):
        ppm = os.path.join(tmp, name + '.ppm')
        self.ctl('s' + ppm)
        for _ in range(100):
            if os.path.exists(ppm) and os.path.getsize(ppm) >= 240 * 160 * 3: break
            time.sleep(0.05)
        time.sleep(0.2)
        subprocess.run(['convert', ppm, '-scale', '300%', os.path.join(SHOTS, name + '.png')])


gba = MGBALink(os.path.join(tmp, 'unused'), 'flash128', log=lambda m: print('  [gba]', m))
wrapper = os.path.join(tmp, 'mgba.sh')         # "mGBA": mGBA --script LUA ROM
open(wrapper, 'w').write(f'#!/bin/sh\n[ "$1" = "--script" ] || exit 1\nTHROTTLE_US=1000 exec {RUNLUA} "$3" "$2" 150000 2>>{tmp}/mgba.log\n')
os.chmod(wrapper, 0o755)
env = dict(os.environ, GBLINK_PORT=gba.port, GBLINK_SYNC_HOME=os.path.join(tmp, 'home'), GBLINK_TEST_HOOKS='47951',
           GBLINK_TEST_PICK=games, LIBGL_ALWAYS_SOFTWARE='1', GBLINK_MULTI='1')
app = subprocess.Popen([APP], env=env, stdout=open(os.path.join(tmp, 'app.log'), 'w'), stderr=subprocess.STDOUT, cwd=tmp)
import atexit
atexit.register(lambda: app.poll() is None and app.kill())

sock = None
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47951)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
def cmd(c):
    sock.sendall((c + '\n').encode()); return rf.readline().strip()
def state(): return json.loads(cmd('state'))
def wait(cond, timeout=30, what=''):
    end = time.time() + timeout
    while time.time() < end:
        s = state()
        if cond(s): return s
        time.sleep(0.15)
    print(open(os.path.join(tmp, 'app.log')).read()[-3000:])
    raise SystemExit('timed out waiting for ' + what + '\n' + json.dumps(state())[:600])
def press(k, hold=0.15, gap=0.25):
    gba.keys(k); time.sleep(hold); gba.keys(0); time.sleep(gap)

UP, DOWN, A, B, LEFT, RIGHT, L, R, SELECT, START = 0x40, 0x80, 0x1, 0x2, 0x20, 0x10, 0x200, 0x100, 0x4, 0x8
checks = {}
time.sleep(2)
cmd('settings ' + json.dumps({'mgba': wrapper}))
cmd('click browse-folder')
wait(lambda s: s['Settings']['gamesFolder'] == games, what='games folder')
cmd('click setup-done')
time.sleep(0.5)
wait(lambda s: any(g['ROM'] == rom for g in (s['Library'] or [])), 15, 'game listed')
time.sleep(1.0)
cmd('game ' + rom); time.sleep(0.5)
print('selected:', cmd('selected'))
cmd('click play')
s = wait(lambda s: s['Phase'] != 'home', 10, 'play')
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 20, 'switch-on prompt')
gba.power_on_bios()
wait(lambda s: s['Phase'] == 'play', 60, 'playing')
wait(lambda s: s['Controller']['ScriptConnected'] and s['Controller']['Online'], 30, 'script + GBA')
time.sleep(2.5)
if False:                       # just controller mode and holding SELECT
    press(A); time.sleep(1.5)
    checks['controller mode chosen'] = state()['Controller']['GBAShows'] == ''
    press(SELECT, hold=0.1); time.sleep(1.0)
    checks['a quick SELECT stays in controller mode'] = state()['Controller']['GBAShows'] == ''
    gba.keys(SELECT); time.sleep(1.2); gba.keys(0); time.sleep(1.5); gba.shot('8_combo_menu')
    checks['hold SELECT opens the menu'] = state()['Controller']['GBAShows'] == 'its menu'
    app.terminate(); app.wait(); gba.power_off()
    for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
    sys.exit(0 if all(checks.values()) else 1)


press(A); time.sleep(1.0)                                 # "Use GBA as controller"
checks['controller mode'] = state()['Controller']['GBAShows'] == ''
s = wait(lambda s: s['WCGame'] != '' or s['WCWhy'] != '', 30, 'ROM check')
checks['verified Emerald can take Wonder Cards'] = s['WCGame'] == 'BPEE0'
end = time.time() + 95                                    # through the intro, to the truck
while time.time() < end:
    gba.keys(A); time.sleep(0.08); gba.keys(0); time.sleep(0.08)
    if int(time.time()) % 6 == 0: gba.keys(START); time.sleep(0.08); gba.keys(0); time.sleep(0.08)
for _ in range(12): press(B, hold=0.1, gap=0.3)                 # out of any menu the mashing opened
cmd('click wc-open'); time.sleep(0.5)
cmd('click wc-tick:' + card('Master Ball')); time.sleep(0.5)
def ticks():
    try: return json.load(open(os.path.splitext(rom)[0] + '.gblink.json')).get('wonderCards')
    except Exception: return None
for _ in range(30):
    if ticks() == ['Master Ball']: break
    time.sleep(0.2)
print('ticks saved:', ticks())
checks['the tick is remembered'] = ticks() == ['Master Ball']
cmd('click wc-use')
end = time.time() + 60                                    # A through its messages
while time.time() < end:
    s = state()
    if s['WCStatus'].startswith('Used') or s['WCStatus'].startswith('Couldn') or 'first' in s['WCStatus']: break
    gba.keys(B); time.sleep(0.1); gba.keys(0); time.sleep(0.15)
    gba.keys(A); time.sleep(0.1); gba.keys(0); time.sleep(0.15)
print('status:', s['WCStatus'])
checks['Master Ball used now'] = s['WCStatus'].startswith('Used')
cmd('click wc-tick:' + card('Master Ball'))
cmd('click wc-tick:' + card('Fast Forward 2× (Press R)'))
cmd('click wc-tick:' + card('Walk Through Walls (Press R)')); time.sleep(0.5)
checks['ticking an "until reset" card unticks the other'] = (card('Fast Forward 2× (Press R)') and
    state()['WCChosen'] == ['Walk Through Walls (Press R)'])
cmd('click wc-tick:' + card('Walk Through Walls (Press R)')); time.sleep(0.3)
cmd('click wc-tick:' + card('Master Ball')); time.sleep(0.3)
if os.environ.get('SHOT'): subprocess.run(['import', '-window', 'root', os.environ['SHOT']])
cmd('click wc-add')
s = wait(lambda s: s['WCStatus'].startswith('Received') or s['WCStatus'].startswith('Couldn'), 30, 'card received')
checks['Master Ball received by the game'] = s['WCStatus'].startswith('Received')
cmd('click wc-close'); time.sleep(0.3); cmd('click settings'); time.sleep(1.0)
if os.environ.get('SHOT'): subprocess.run(['import', '-window', 'root', os.environ['SHOT'].replace('.png', '_settings.png')])
cmd('click settings-done')
log = open(os.path.join(tmp, 'app.log')).read()
app.terminate(); app.wait()
gba.power_off()
for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
print(log[-2500:])
print(open(os.path.join(tmp, 'mgba.log')).read()[-1500:])
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
