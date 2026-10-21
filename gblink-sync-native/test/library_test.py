"""The game list's No-Intro check: a real ROM (pass its path) shows Verified, a copy with one byte
changed shows "Not a good dump", an unknown game shows "Not in No-Intro"; then a DAT file is
loaded from Settings and used.
  DISPLAY=:79 python3 library_test.py APP SHOTS ROM.gba"""
import json, os, shutil, socket, subprocess, sys, tempfile, time
APP, SHOTS, ROM = sys.argv[1], sys.argv[2], sys.argv[3]
os.makedirs(SHOTS, exist_ok=True)
tmp = tempfile.mkdtemp()
games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
rom = bytearray(open(ROM, 'rb').read())
open(os.path.join(games, 'Pokemon Ruby.gba'), 'wb').write(rom)
rom[0x100000] ^= 0xFF
open(os.path.join(games, 'Pokemon Ruby (patched).gba'), 'wb').write(rom)
h = bytearray(0x400); h[0xA0:0xB0] = b'MY HOMEBREW ZZZZ'
open(os.path.join(games, 'My Homebrew.gba'), 'wb').write(h)
dat = os.path.join(tmp, 'tiny.dat')     # a Datomatic-style (Logiqx XML) DAT with only the homebrew in it
import hashlib
open(dat, 'w').write('<?xml version="1.0"?><datafile><header><name>Nintendo - Game Boy Advance</name><version>20991231</version></header>'
                     f'<game name="My Homebrew (World)"><rom name="My Homebrew (World).gba" size="1024" crc="00000000" sha1="{hashlib.sha1(h).hexdigest()}"/></game></datafile>')
home = os.path.join(tmp, 'home'); os.makedirs(home)
open(os.path.join(home, 'settings.json'), 'w').write(json.dumps({'mgba': '/bin/true', 'gamesFolder': games, 'setupDone': True}))
env = dict(os.environ, GBLINK_PORT='/dev/null', GBLINK_SYNC_HOME=home, GBLINK_TEST_HOOKS='47980', LIBGL_ALWAYS_SOFTWARE='1',
           GBLINK_MULTI='1', GBLINK_TEST_PICK=dat)
app = subprocess.Popen([APP], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=tmp)
for _ in range(100):
    try: sock = socket.create_connection(('127.0.0.1', 47980)); break
    except OSError: time.sleep(0.1)
rf = sock.makefile('r')
def cmd(c): sock.sendall((c + '\n').encode()); return rf.readline().strip()
def wait(cond, timeout=60, what=''):
    end = time.time() + timeout
    while time.time() < end:
        s = json.loads(cmd('state'))
        if cond(s): return s
        time.sleep(0.2)
    raise SystemExit('timed out waiting for ' + what)
def shot(n): time.sleep(0.8); subprocess.run(['import', '-window', 'root', '-crop', '920x740+0+0', f'{SHOTS}/{n}.png'])
by = lambda s: {g['Name']: g for g in s['Library']}
s = wait(lambda s: len(s['Library']) == 3 and all(g['Verify'] for g in s['Library']), 60, 'checked')
g = by(s)
print({k: (v['Verify'], v['NoIntro']) for k, v in g.items()})
checks = {
    'real ROM: Verified, with its No-Intro name': g['Pokemon Ruby']['Verify'] == 'ok' and 'Rev 2' in g['Pokemon Ruby']['NoIntro'],
    'one byte changed: not a good dump': g['Pokemon Ruby (patched)']['Verify'] == 'bad',
    'unknown game: not in No-Intro': g['My Homebrew']['Verify'] == 'unknown',
    'database shown in Settings': '2026.10.07' in s['DATInfo'] and 'built in' in s['DATInfo'],
}
cmd('game ' + g['Pokemon Ruby']['ROM']); shot('l1_library')
cmd('click settings'); shot('l2_settings')
cmd('click load-dat')
s = wait(lambda s: '20991231' in s['DATInfo'], 20, 'DAT loaded')
s = wait(lambda s: by(s)['My Homebrew']['Verify'] == 'ok', 20, 'reclassified')
shot('l3_dat_loaded')
checks['a loaded DAT is used'] = by(s)['My Homebrew']['NoIntro'] == 'My Homebrew (World)' and 'loaded from a DAT file' in s['DATInfo']
app.terminate(); app.wait()
for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
shutil.rmtree(tmp)
sys.exit(0 if all(checks.values()) else 1)
