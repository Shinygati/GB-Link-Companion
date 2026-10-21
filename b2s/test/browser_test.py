"""Click through the real web page in headless Chromium, with the USB connection swapped for a
bridge to the native simulator (GBA flasher code + simulated flash chip)."""
import http.server, os, subprocess, threading, functools, sys, random
from playwright.sync_api import sync_playwright

ROOT = os.path.dirname(os.path.abspath(__file__))
WEB = os.path.join(ROOT, '..', 'web')
dump = '/tmp/b2s_browser_dump.bin'
sim = subprocess.Popen([os.path.join(ROOT, '..', 'sim', 'sim'), 'amd', '2', '0', '4', '10', dump, '3'],
                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, env={**os.environ, 'SIM_SAVE': 'flash128'})
srv = http.server.ThreadingHTTPServer(('127.0.0.1', 8765), functools.partial(http.server.SimpleHTTPRequestHandler, directory=WEB))
threading.Thread(target=srv.serve_forever, daemon=True).start()

rnd = random.Random(5)
rom = bytearray(rnd.getrandbits(8) for _ in range(700001))
rom[0xA0:0xB0] = b'TESTROM\0\0\0\0\0BTST'
open('/tmp/test_rom.gba', 'wb').write(rom)

def sim_write(data): sim.stdin.write(bytes(data)); sim.stdin.flush()
def sim_read(n): return list(sim.stdout.read(n))

MOCK = """() => {
  const P = UsbConnection.prototype;
  P.connect = async function () { this.isConnected = true; this.isNewFirmware = true; };
  P.disconnect = async function () { this.isConnected = false; };
  P.setMode = async function () { return true; };
  P.setTimingConfig = async function () { return true; };
  P.writeBytes = async function (tx) { await window.simWrite(Array.from(tx)); };
  P.readBytesRaw = async function (n) { return new Uint8Array(await window.simRead(n)); };
}"""
errors = []
with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page()
    pg.on('pageerror', lambda e: errors.append(str(e)))
    pg.on('console', lambda m: m.type == 'error' and errors.append(m.text))
    pg.expose_function('simWrite', sim_write)
    pg.expose_function('simRead', sim_read)
    pg.add_init_script("if (!('usb' in navigator)) Object.defineProperty(navigator, 'usb', {value: {}});")
    pg.goto('http://127.0.0.1:8765/index.html')
    pg.evaluate(MOCK)
    pg.click('#btn-connect')
    pg.wait_for_selector('#usb-status.connected', timeout=5000)
    pg.click('#btn-detect')
    pg.wait_for_selector('#chip-info.show', timeout=30000)
    print('chip info:', pg.inner_text('#chip-info'))
    pg.set_input_files('#file-input', '/tmp/test_rom.gba')
    pg.wait_for_function("!document.getElementById('btn-flash').disabled")
    pg.click('#btn-flash')
    pg.wait_for_function("document.getElementById('progress-phase').textContent === 'Done' || [...document.querySelectorAll('.log-entry.error')].length > 0", timeout=300000)
    print('progress:', pg.inner_text('#progress-phase'), pg.inner_text('#progress-text'))
    print('--- log ---'); print(pg.inner_text('#log-container'))
    # general multiboot section
    open('/tmp/test_mb.mb', 'wb').write(bytes(range(256)) * 64)
    pg.evaluate("() => { window.GBAMultiboot.multiboot = async (usb, data, log) => { window.mbSent = data.byteLength; log('stub multiboot sent', 'success'); return true; }; }")
    pg.set_input_files('#mb-file-input', '/tmp/test_mb.mb')
    pg.wait_for_function("!document.getElementById('btn-send-mb').disabled")
    pg.click('#btn-send-mb')
    pg.wait_for_function("window.mbSent === 16384")
    print('multiboot section: sent', pg.evaluate('window.mbSent'), 'bytes')
    # save backup / restore
    pg.on('dialog', lambda d: d.accept())
    pg.click('#btn-save-detect')
    pg.wait_for_selector('#save-info.show', timeout=60000)
    print('save info:', pg.inner_text('#save-info'))
    with pg.expect_download(timeout=120000) as dl:
        pg.click('#btn-save-backup')
    path = dl.value.path(); backup = open(path, 'rb').read()
    expect = bytes(((i * 7 + (i >> 8) * 13) & 0xFF) for i in range(0x20000))
    print('backup:', dl.value.suggested_filename, len(backup), 'bytes, matches chip:', backup == expect)
    newsave = bytes(((i * 31 + 7) >> 3) & 0xFF for i in range(0x20000))
    open('/tmp/new.sav', 'wb').write(newsave)
    pg.set_input_files('#save-file-input', '/tmp/new.sav')
    pg.wait_for_function("document.getElementById('save-progress-phase').textContent === 'Restored' || [...document.querySelectorAll('.log-entry.error')].length > 0", timeout=120000)
    print('restore phase:', pg.inner_text('#save-progress-phase'))
    pg.screenshot(path='/tmp/b2s_page.png', full_page=True)
    b.close()
sim.stdin.close(); sim.wait()
got = open(dump, 'rb').read()
want = bytes(rom) + b'\xff' * ((-len(rom)) % 4)
savechip = open(dump + '.sav', 'rb').read()
print('restored save on chip matches file:', savechip == newsave)
ok = got[:len(want)] == want and not errors and backup == expect and savechip == newsave
print('page errors:', errors)
print('PASS' if ok else 'FAIL')
srv.shutdown()
sys.exit(0 if ok else 1)
