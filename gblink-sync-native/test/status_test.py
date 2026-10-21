"""End-to-end test of the GBA's status screens, with real code everywhere it matters:
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

APP, SHOTS, KEY, SCEN = sys.argv[1:5]
MGBA_LINK = os.environ.get('MGBA_LINK', os.path.join(HERE, '../../b2s/test/mgba_link'))
RUNLUA = os.environ.get('RUNLUA', os.path.join(HERE, 'status/runlua'))
HELPER = os.path.join(HERE, '../assets/gblink_sync_mb.gba')
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()
games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
rom = os.path.join(games, f'Pokemon {KEY}.gba')
subprocess.run([sys.executable, os.path.join(HERE, 'status/mkrom.py'), KEY, rom, SCEN], check=True)
cart_rom = os.path.join(tmp, 'cart.gba'); shutil.copy(rom, cart_rom)     # the cart in the GBA
if os.environ.get('WITH_SYM'):                 # your own build: its .sym next to the ROM
    shutil.copy(os.environ['WITH_SYM'], os.path.splitext(rom)[0] + '.sym')
OWN = KEY.endswith('QoL') or bool(os.environ.get('WITH_SYM'))   # no RNG screen on your own builds


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

    def shot(self, name):
        ppm = os.path.join(tmp, name + '.ppm')
        self.ctl('s' + ppm)
        for _ in range(100):
            if os.path.exists(ppm) and os.path.getsize(ppm) >= 240 * 160 * 3: break
            time.sleep(0.05)
        time.sleep(0.2)
        subprocess.run(['convert', ppm, '-scale', '300%', os.path.join(SHOTS, name + '.png')])


gba = MGBALink(os.path.join(tmp, 'unused'), 'flash128', log=lambda m: print('  [gba]', m))
wrapper = os.path.join(tmp, 'mgba.sh')         # "mGBA": mGBA --script LUA ROM
open(wrapper, 'w').write(f'#!/bin/sh\n[ "$1" = "--script" ] || exit 1\nTHROTTLE_US=16000 exec {RUNLUA} "$3" "$2" 4300 2>>{tmp}/mgba.log\n')
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
gba.shot('1_menu')
checks['GBA starts on its menu'] = state()['Controller']['GBAShows'] == 'its menu'
if os.environ.get('QUICK_CTL'):                       # just controller mode and holding SELECT
    press(A); time.sleep(1.5)
    checks['controller mode chosen'] = state()['Controller']['GBAShows'] == ''
    press(SELECT, hold=0.1); time.sleep(1.0)
    checks['a quick SELECT stays in controller mode'] = state()['Controller']['GBAShows'] == ''
    gba.keys(SELECT); time.sleep(1.2); gba.keys(0); time.sleep(1.5); gba.shot('8_combo_menu')
    checks['hold SELECT opens the menu'] = state()['Controller']['GBAShows'] == 'its menu'
    app.terminate(); app.wait(); gba.power_off()
    for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
    sys.exit(0 if all(checks.values()) else 1)

press(DOWN); press(A); time.sleep(2.0)                 # RNG state (own builds: Wild, there's no RNG)
if OWN:
    checks['own build: no RNG screen, Wild is first'] = state()['Controller']['GBAShows'] == 'Wild Pokémon'
    gba.shot('3_wild')
else:
    gba.shot('2_rng')
    checks['RNG screen chosen'] = state()['Controller']['GBAShows'] == 'RNG state'
    press(RIGHT); time.sleep(2.0); gba.shot('3_wild')     # Right: the next screen
gba.keys(SELECT); time.sleep(0.9); gba.keys(0); time.sleep(1.0)   # hold SELECT: play while it shows
gba.shot('3b_wild_playing')
checks['hold SELECT: buttons control mGBA on a status screen'] = state()['Controller']['Passthru']
press(B); time.sleep(0.5)
checks['...and B goes to the game, not the menu'] = state()['Controller']['GBAShows'] == 'Wild Pokémon'
gba.keys(SELECT); time.sleep(0.9); gba.keys(0); time.sleep(1.0)   # hold SELECT again: stop
checks['hold SELECT again: back to the screen\'s buttons'] = not state()['Controller']['Passthru']
press(RIGHT); time.sleep(2.0); gba.shot('4_trainer')
press(DOWN); time.sleep(1.0); gba.shot('4b_trainer_scrolled')
press(RIGHT); time.sleep(2.0); gba.shot('5_map')
time.sleep(0.5); gba.shot('5b_map_blink')
press(A); time.sleep(1.5); press(DOWN); time.sleep(0.8); press(UP); time.sleep(1.5); gba.shot('5c_map_cursor')   # A: a cursor; move it
press(A); time.sleep(3.0); gba.shot('5d_map_picture')     # A: a picture of the place
press(B); time.sleep(1.0); press(B); time.sleep(1.0)        # B: back to the cursor, B: back to the map
time.sleep(2.5); gba.shot('5d2_map_back')                   # your icon is still there
press(RIGHT); time.sleep(1.5); gba.shot('5e_encounters')   # Right: the encounter counter
press(START); press(A); press(UP); press(UP); press(A); time.sleep(1.0)   # count on; set it to 2
gba.shot('5f_encounters_set')
st_json = json.load(open(os.path.splitext(rom)[0] + '.gblink.json'))
checks['encounter count set and saved'] = st_json.get('encounters') == 2 and st_json.get('encountersOn') is True
press(LEFT); time.sleep(1.0)                                  # back to the map
press(B); time.sleep(1.0); gba.shot('6_menu_again')
checks['B goes back to the menu'] = state()['Controller']['GBAShows'] == 'its menu'
for _ in range(3 if OWN else 4): press(UP)            # back to "Use GBA as controller"
press(A); time.sleep(1.5); gba.shot('7_controller')
checks['controller mode chosen'] = state()['Controller']['GBAShows'] == ''
gba.keys(SELECT); time.sleep(1.2); gba.keys(0); time.sleep(1.5)
gba.shot('8_combo_menu')
checks['hold SELECT opens the menu'] = state()['Controller']['GBAShows'] == 'its menu'
wait(lambda s: s['Phase'] in ('done', 'finish'), 90, 'mGBA closing')   # "mGBA" quits after its frames
time.sleep(4); gba.shot('9_mgba_closed')
settings = json.load(open(os.path.join(tmp, 'home', 'settings.json')))
checks['mode remembered'] = settings.get('statusMode') == 'menu'
log = open(os.path.join(tmp, 'app.log')).read()
checks['script found the game'] = 'Status screens: reading Pok' in log
if os.environ.get('WITH_SYM'):
    checks['used the .sym next to the ROM'] = 'using the addresses in' in log and 'Own Build' in log
elif KEY.endswith('QoL'):
    checks['recognised the QoL build'] = 'Qol' in log or 'QoL' in log
app.terminate(); app.wait()
gba.power_off()
for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
print(log[-6000:])
if os.environ.get('APPLOG'): shutil.copy(os.path.join(tmp, 'app.log'), os.environ['APPLOG'])
print(open(os.path.join(tmp, 'mgba.log')).read()[-800:])
print('--- mgba_link:', open(os.path.join(tmp, 'mgba_link.log')).read()[-1500:])
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
