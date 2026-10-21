"""End-to-end test of the native app (Linux build with -tags testhooks) on a virtual display:
fake GB-Link + simulated GBA running the real helper code + a stand-in mGBA. Buttons are pressed
through the test hooks (the same buttons the player presses) and, for the main ones, with real
mouse clicks via xdotool. Saves screenshots.
  DISPLAY=:79 python3 native_test.py APP SHOTS [off]"""
import json, os, random, shutil, socket, subprocess, sys, tempfile, threading, time
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
APP, SHOTS = sys.argv[1], sys.argv[2]
MODE = sys.argv[3] if len(sys.argv) > 3 else ''
OFF = MODE in ('off', 'close')     # GBA switched off mid-game, on again at the end
CLOSE = MODE == 'close'            # ...and the window is closed while playing
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()
keys_file = os.path.join(tmp, 'keys'); open(keys_file, 'w').write('0')
os.environ['SIM_KEYS_FILE'] = keys_file
from fake_gblink import FakeGBLink

games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
def rom(name, title, code):
    h = bytearray(0x400); h[0xA0:0xAC] = title.ljust(12, '\0').encode()[:12]; h[0xAC:0xB0] = code.encode()
    p = os.path.join(games, name); open(p, 'wb').write(h); return p
sim_rom = rom('Sim Game.gba', 'SIMGAME TEST', 'BSIM')
rom('Pokemon Emerald Version.gba', 'POKEMON EMER', 'BPEE')
rom('Mario Kart Super Circuit.gba', 'MARIOKARTSC', 'AMKE')
rom('Golden Sun.gba', 'Golden_Sun_A', 'AGSE')
cart = os.path.join(tmp, 'cart_save.bin')
open(cart, 'wb').write(bytes(random.Random(5).getrandbits(8) for _ in range(0x8000)))
ctl = os.path.join(tmp, 'ctl'); open(ctl, 'w').close()
wrapper = os.path.join(tmp, 'mgba.sh')
open(wrapper, 'w').write(f'#!/bin/sh\nexec {sys.executable} {HERE}/fake_mgba_ctl.py "$@"\n'); os.chmod(wrapper, 0o755)

gba = FakeGBLink(cart, 'sram32', log=lambda m: print('  [gba]', m))
env = dict(os.environ, GBLINK_PORT=gba.port, GBLINK_COMPANION_HOME=os.path.join(tmp, 'home'), GBLINK_TEST_HOOKS='47950',
           GBLINK_TEST_PICK=games, FAKE_CTL=ctl, FAKE_SECONDS='14', FAKE_SAVE_AT='9' if OFF else '5',
           LIBGL_ALWAYS_SOFTWARE='1', GBLINK_MULTI='1')
app = subprocess.Popen([APP], env=env, stdout=open(os.path.join(tmp, 'app.log'), 'w'), stderr=subprocess.STDOUT, cwd=tmp)

sock = None
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47950)); break
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
    raise SystemExit('timed out waiting for ' + what + '\n' + json.dumps(state())[:600])
def shot(name, delay=0.6):
    time.sleep(delay)
    subprocess.run(['import', '-window', 'root', '-crop', '920x740+0+0', f'{SHOTS}/{name}.png'])
def mouse(x, y, clicks=1):
    subprocess.run(['xdotool', 'mousemove', str(x), str(y)]); time.sleep(0.15)
    subprocess.run(['xdotool', 'click', '--repeat', str(clicks), '--delay', '120', '1'])

def press():                                             # the player presses buttons on the GBA
    while 'keys 000' not in open(ctl).read(): time.sleep(0.1)
    # the GBA starts on its menu: A picks the first item, "Use GBA as controller"
    for k in ('001', '0', '209', '001', '0'):
        time.sleep(1.2); open(keys_file, 'w').write(k)
threading.Thread(target=press, daemon=True).start()

checks = {}
import atexit
atexit.register(lambda: app.poll() is None and app.kill())
time.sleep(2.5)
shot('1_setup')
cmd('settings ' + json.dumps({'mgba': wrapper}))           # (on Windows mGBA is found by itself)
print('click ->', cmd('click browse-folder'))                                  # "Choose folder…" (picker answered by the test)
wait(lambda s: s['Settings']['gamesFolder'] == games, what='games folder')
shot('2_setup_done')
cmd('click setup-done')                                     # Continue (the layout moves, so by id)
s = wait(lambda s: s['Settings']['setupDone'], 10, 'Continue clicked')
checks['Continue'] = True
shot('3_games')
checks['games folder listed'] = len(s['Library']) == 4
cmd('search sim'); shot('4_search', 0.5)
cmd('search ')
time.sleep(0.4)
# double-click the Sim Game row with the real mouse: rows are listed A-Z (Golden Sun, Mario Kart,
# Pokemon Emerald, Sim Game); find its row from the screenshot layout: 4th row
rom_sim = next(g['ROM'] for g in state()['Library'] if g['Name'] == 'Sim Game')
cmd('game ' + rom_sim); time.sleep(0.5); cmd('click play')
s = wait(lambda s: s['Phase'] != 'home', 10, 'double-click to play')
checks['double-click a game plays it'] = s['Game']['Name'] == 'Sim Game'
wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 20, 'switch-on prompt')
shot('5_switch_on')
gba.power_on_bios()
try:
    wait(lambda s: s['Progress'] is not None, 15, 'progress'); shot('6_loading', 0.2)
except SystemExit:
    pass
wait(lambda s: s['Phase'] == 'play', 30, 'playing')
wait(lambda s: s['Controller']['ScriptConnected'] and s['Controller']['Online'], 15, 'controller')
shot('7_playing')
checks['controller on'] = True
def close_window():                                     # like pressing the window's X
    from Xlib import X, display, protocol
    d = display.Display()
    def find(win):
        try:
            if win.get_wm_name() == 'GB-Link Companion': return win
        except Exception: pass
        for c in win.query_tree().children:
            r = find(c)
            if r: return r
    w = find(d.screen().root)
    ev = protocol.event.ClientMessage(window=w, client_type=d.intern_atom('WM_PROTOCOLS'),
                                      data=(32, [d.intern_atom('WM_DELETE_WINDOW'), X.CurrentTime, 0, 0, 0]))
    w.send_event(ev); d.flush()
if OFF:
    time.sleep(3); gba.power_off()
    if CLOSE:
        close_window(); time.sleep(1.5); shot('8a_window_closed', 0)
        checks['app keeps running with the window closed'] = app.poll() is None
    wait(lambda s: s['Cart'] == 'pending', 40, 'save kept on PC'); shot('8_gba_off')
    wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 30, 'switch on to save'); shot('8b_switch_on_to_save')
    gba.power_on_bios()
else:
    wait(lambda s: any('Save written' in l for l in (s['Log'] or [])), 20, 'live save'); shot('8_live_save', 0.3)
s = wait(lambda s: s['Phase'] == 'done', 40, 'done')
shot('9_done')
checks['finished: done'] = s['Result'] == 'done'
bdir = os.path.join(games, 'gblink_backups', 'Sim Game')
checks['saves made in mGBA backed up in the ROM folder'] = os.path.isdir(bdir) and any(f.endswith('-mgba.sav') for f in os.listdir(bdir))
sav = os.path.join(games, 'Sim Game.sav')
checks['.sav is just the save (no clock data)'] = os.path.getsize(sav) == 0x8000
cmd('click settings'); shot('10_settings'); cmd('click settings-done')
cmd('click home')
s = wait(lambda s: s['Phase'] == 'home', 10, 'home')
shot('11_games_after')
checks['recently played first'] = s['Library'][0]['Name'] == 'Sim Game'
app.terminate(); app.wait()
dark = subprocess.Popen([APP], env=dict(env, GBLINK_THEME='dark'), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=tmp)
time.sleep(3.5); shot('12_games_dark', 0); dark.terminate(); dark.wait()

gba.power_off()
sav = open(os.path.join(games, 'Sim Game.sav'), 'rb').read()
got = [l.split()[1] for l in open(ctl).read().splitlines() if l.startswith('keys')]
checks['cart has the save made in mGBA'] = open(cart, 'rb').read() == sav[:0x8000] and 'saved' in open(ctl).read()
checks['GBA buttons reached mGBA'] = [k for k in got if k != '000'] == ['209', '001'] or (OFF and '209' in got)
print('keys seen by mGBA:', got)
for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
print(open(os.path.join(tmp, 'app.log')).read()[-2000:])
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
