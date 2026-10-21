"""A pretend GB-Link adapter on a pseudo serial port, with a pretend GBA behind it.

The GBA can be OFF (link line idle), in its BIOS waiting for multiboot (a mock that accepts any
image the way the real BIOS handshake does), or RUNNING the multibooted flasher - which is the
real flasher code in ../../b2s/sim/sim, with a save chip that keeps its contents across power
cycles (cart_save file)."""
import os, pty, struct, subprocess, threading, time, tty

SIM = os.path.join(os.path.dirname(os.path.abspath(__file__)), '../../b2s/sim/sim')


class FakeGBLink:
    def __init__(self, cart_save, save_kind='flash128', log=lambda m: None):
        self.master, self.slave = pty.openpty()
        tty.setraw(self.slave)
        self.port = os.ttyname(self.slave)
        self.cart_save, self.save_kind, self.log = cart_save, save_kind, log
        self.state, self.sim, self.mb = 'off', None, None
        self.lock = threading.Lock()
        self.buf = bytearray()
        self.multiboots = 0
        threading.Thread(target=self._loop, daemon=True).start()

    # ------------------------------------------------------------- GBA power
    def power_off(self):
        with self.lock:
            self._stop_sim()
            self.state = 'off'
        self.log('GBA switched off')

    def power_on_bios(self):
        """switch on holding START + SELECT: BIOS waits for multiboot"""
        with self.lock:
            self._stop_sim()
            self.state, self.mb = 'bios', {'phase': 'poll'}
        self.log('GBA switched on (START + SELECT)')

    def _stop_sim(self):
        if self.sim:
            self.sim.stdin.close()
            self.sim.wait()
            dump = self.sim_dump + '.sav'
            if os.path.exists(dump):
                os.replace(dump, self.cart_save)        # the cart keeps its save
            os.remove(self.sim_dump)
            self.sim = None

    def _start_sim(self):
        self.sim_dump = f'/tmp/fake_gblink_{os.getpid()}.bin'
        env = dict(os.environ, SIM_SAVE=self.save_kind, SIM_SAVE_IN=self.cart_save)
        self.sim = subprocess.Popen([SIM, 'amd', '0', '0', os.environ.get('FAKE_CHIP_MB', '1'), '5', self.sim_dump, '11'],
                                    stdin=subprocess.PIPE, stdout=subprocess.PIPE, env=env)
        self.state = 'running'
        self.multiboots += 1
        self.log('flasher started on the GBA')

    # ------------------------------------------------------------- one word in, one out
    def _word(self, w):
        if self.state == 'off':
            return 0xFFFFFFFF
        if self.state == 'running':
            self.sim.stdin.write(struct.pack('>I', w)); self.sim.stdin.flush()
            return struct.unpack('>I', self.sim.stdout.read(4))[0]
        mb = self.mb                                   # mock BIOS multiboot (single 32-bit client)
        ph = mb['phase']
        if ph == 'poll':
            if w == 0x6202: return 0x72020000
            if w == 0x6102: mb.update(phase='header', n=0); return 0x72020000
            return 0
        if ph == 'header':
            mb['n'] += 1
            if mb['n'] == 0x60: mb['phase'] = 'hs'
            return (0x60 - mb['n']) << 24
        if ph == 'hs':
            if w == 0x63D1:
                mb['d1'] = mb.get('d1', 0) + 1
                if mb['d1'] == 2: mb['phase'] = 'crcA'; return 0x735A0000
            return 0x72020000
        if ph == 'crcA': mb['phase'] = 'len'; return 0x73000000
        if ph == 'len':
            mb.update(phase='data', fsize=w * 4 + 0x190, off=0xC0); return 0x00660000
        if ph == 'data':
            r = (mb['off'] & 0xFFFF) << 16
            mb['off'] += 4
            if mb['off'] >= mb['fsize']: mb['phase'] = 'ack'
            return r
        if ph == 'ack':
            if w == 0x0065: return 0x00750000
            if w == 0x0066: mb['phase'] = 'crc'; return 0
            return 0
        if ph == 'crc':
            self._start_sim()
            return 0
        return 0

    # ------------------------------------------------------------- serial framing
    def _loop(self):
        while True:
            try:
                chunk = os.read(self.master, 4096)
            except OSError:
                return
            self.buf += chunk
            while True:
                i = self.buf.find(b'GB')
                if i < 0 or len(self.buf) < i + 5: break
                ch, ln = self.buf[i + 2], self.buf[i + 3] | self.buf[i + 4] << 8
                if len(self.buf) < i + 5 + ln: break
                payload = bytes(self.buf[i + 5:i + 5 + ln]); del self.buf[:i + 5 + ln]
                if ch != 1:
                    continue                            # commands (mode, timing, voltage)
                with self.lock:
                    if self.state == 'running':          # whole batch to the simulator at once
                        t0 = time.time()
                        self.sim.stdin.write(payload); self.sim.stdin.flush()
                        out = self.sim.stdout.read(len(payload))
                        if os.environ.get('FAKE_SLOW_LOG') and time.time() - t0 > 0.3:
                            self.log(f'slow batch: {time.time() - t0:.2f} s for {len(payload) // 4} words')
                    else:
                        out = b''.join(struct.pack('>I', self._word(w)) for w in struct.unpack('>%dI' % (len(payload) // 4), payload))
                os.write(self.master, b'GB' + bytes([1, len(out) & 0xFF, len(out) >> 8]) + out)
