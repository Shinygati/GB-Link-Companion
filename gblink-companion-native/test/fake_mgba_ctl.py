"""Stands in for mGBA's side of controller mode: does what gblink_controller.lua does (connect to
the launcher on localhost and read the button states), and logs what it gets to FAKE_CTL.
With FAKE_OLD set it behaves like mGBA 0.10: '--script' is an unknown option, so it quits."""
import os, re, socket, sys, time
args, ctl = sys.argv[1:], os.environ['FAKE_CTL']
log = lambda m: open(ctl, 'a').write(m + '\n')
if '--script' in args:
    if os.environ.get('FAKE_OLD'):
        sys.exit(1)
    lua = args[args.index('--script') + 1]
else:
    lua = os.environ.get('FAKE_LUA_HINT')             # "the user loaded it via Tools > Scripting"
log('started' + (' with-script' if '--script' in args else ''))
s = None
if lua:
    port = int(re.search(r'local PORT = (\d+)', open(lua).read()).group(1))
    for _ in range(50):
        try:
            s = socket.create_connection(('127.0.0.1', port)); break
        except OSError:
            time.sleep(0.2)
    s.settimeout(0.2)
t0 = time.time()
buf, last, end = b'', None, t0 + float(os.environ.get('FAKE_SECONDS', '10'))
save_at = float(os.environ.get('FAKE_SAVE_AT', '0'))
while time.time() < end:
    if save_at and time.time() - t0 > save_at:          # an in-game save
        save_at = 0
        sav = os.path.splitext(args[-1])[0] + '.sav'
        d = bytearray(open(sav, 'rb').read())
        for i in (range(0, len(d), 1000) if os.environ.get('FAKE_SAVE_ALL') else (100,)): d[i] ^= 0xFF
        if os.environ.get('FAKE_RTC_FOOTER') and len(d) % 256 == 0:   # mGBA's clock data after the save
            d += bytes(16)
        open(sav, 'wb').write(d)
        log('saved')
    if s is None:
        time.sleep(0.1); continue
    try:
        d = s.recv(512)
        if not d: break
        buf += d
    except socket.timeout:
        pass
    *lines, buf = buf.split(b'\n')
    for l in lines:
        if not l.startswith(b'K'):
            continue                                  # M lines: which status screen the GBA shows
        k = int(l[1:], 16)
        if k != last:
            log(f'keys {k:03x}'); last = k
if s: s.close()
