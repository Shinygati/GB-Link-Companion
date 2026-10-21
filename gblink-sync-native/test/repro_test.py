"""Backing up a game from a 32 MB repro ("Chis") flash cart, with a real ROM (pass its path):
  1. erased flash after the game: the size is measured (no question), the backup matches the ROM
  2. an older game left in the flash after it: the app says the size is a guess and offers both
  DISPLAY=:79 python3 repro_test.py APP SHOTS ROM.gba"""
import json, os, shutil, socket, subprocess, sys, tempfile, time
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
APP, SHOTS, ROM = sys.argv[1], sys.argv[2], sys.argv[3]
os.makedirs(SHOTS, exist_ok=True)
rom = open(ROM, 'rb').read()
os.environ.update(SIM_ROM_FILE=ROM, FAKE_CHIP_MB='32')
from fake_gblink import FakeGBLink
checks = {}

def run(name, fill_ff, mode, rom_file=ROM):
    tmp = tempfile.mkdtemp()
    os.environ['SIM_ROM_FILE'] = rom_file
    if fill_ff: os.environ['SIM_FILL_FF'] = '1'
    else: os.environ.pop('SIM_FILL_FF', None)
    games = os.path.join(tmp, 'GBA Games'); os.makedirs(games)
    cart = os.path.join(tmp, 'cart_save.bin'); open(cart, 'wb').write(bytes(0x8000))
    gba = FakeGBLink(cart, 'sram32', log=lambda m: None)
    home = os.path.join(tmp, 'home'); os.makedirs(home)
    open(os.path.join(home, 'settings.json'), 'w').write(json.dumps({'mgba': '/bin/true', 'gamesFolder': games, 'setupDone': True}))
    env = dict(os.environ, GBLINK_PORT=gba.port, GBLINK_SYNC_HOME=home, GBLINK_TEST_HOOKS='47970', LIBGL_ALWAYS_SOFTWARE='1', GBLINK_MULTI='1')
    app = subprocess.Popen([APP], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=tmp)
    for _ in range(100):
        try: sock = socket.create_connection(('127.0.0.1', 47970)); break
        except OSError: time.sleep(0.1)
    rf = sock.makefile('r')
    def cmd(c): sock.sendall((c + '\n').encode()); return rf.readline().strip()
    def wait(cond, timeout=30, what=''):
        end = time.time() + timeout
        while time.time() < end:
            s = json.loads(cmd('state'))
            if cond(s): return s
            time.sleep(0.2)
        raise SystemExit('timed out waiting for ' + what)
    time.sleep(2)
    cmd('click backup')
    wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'power', 20, 'switch on')
    gba.power_on_bios()
    s = wait(lambda s: s['Prompt'] and s['Prompt']['Kind'] == 'question', 120, 'confirm')
    time.sleep(0.6); subprocess.run(['import', '-window', 'root', '-crop', '920x740+0+0', f'{SHOTS}/{name}.png'])
    p = s['Prompt']
    labels = [b['Label'] for b in p['Buttons']]
    print(f'{name}: "{p["Text"][:150]}..." buttons: {labels}')
    def finish_and_check(tag):
        s = wait(lambda s: s['Phase'] == 'done', 1500, 'done')
        v = s['Verify'] or {}
        print('  verify:', v.get('Title'), '|', v.get('Text'))
        checks[tag + ': backup = the ROM, 16 MB'] = s['Result'] == 'backup' and open(s['BackupPath'], 'rb').read() == rom
        checks[tag + ': verified against No-Intro'] = v.get('Status') == 'ok' and 'Ruby Version' in v.get('Text', '')
        time.sleep(0.6); subprocess.run(['import', '-window', 'root', '-crop', '920x740+0+0', f'{SHOTS}/{tag}_done.png'])
        return s
    if mode == 'erased':
        checks[name + ': 16 MB, no question'] = '(16 MB)' in p['Text'] and 'Back it up' in labels
        cmd('answer go')
        finish_and_check(name)
    elif mode == 'old':
        # the game code is in No-Intro's list (AXVE = 16 MB), so its size is used: no question
        checks[name + ': size from No-Intro (AXVE = 16 MB)'] = 'No-Intro lists this game (AXVE) as 16 MB' in p['Text'] and labels[0] == 'Back it up'
        cmd('answer go')
        s = finish_and_check(name)
        log = s['Log'] or []
        cmd('click copy-log'); time.sleep(1.0)
        clip = subprocess.run(['xclip', '-o', '-selection', 'clipboard'], capture_output=True, text=True, timeout=5).stdout
        checks[name + ': Copy log puts the log on the clipboard'] = clip.strip() == '\n'.join(json.loads(cmd('state'))['Log']).strip()
    else:
        # a game code No-Intro doesn't know, with old data after it: the app asks
        checks[name + ': offers 16 MB or all 32 MB'] = labels[:2] == ['Back up 16 MB', 'Back up all 32 MB']
        checks[name + ': log says "(possible repro cart.)"'] = any(l.endswith('16 MB (possible repro cart.)') for l in (s['Log'] or []))
        cmd('answer cancel')
        wait(lambda s: s['Phase'] == 'done', 30, 'cancelled')
    app.terminate(); app.wait(); gba.power_off(); shutil.rmtree(tmp)
    time.sleep(1)

run('repro_erased', True, 'erased')
run('repro_old_data', False, 'old')
unk = os.path.join(tempfile.mkdtemp(), 'unknown.gba')
u = bytearray(rom); u[0xAC:0xB0] = b'ZZZZ'; open(unk, 'wb').write(u)
run('repro_unknown_code', False, 'unknown', unk)
for k, v in checks.items(): print(('PASS ' if v else 'FAIL ') + k)
sys.exit(0 if all(checks.values()) else 1)
