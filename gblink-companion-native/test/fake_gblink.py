"""A pretend GB-Link adapter on a pseudo serial port, with a pretend GBA behind it.

The GBA can be OFF (link line idle), in its BIOS waiting for multiboot (a mock that accepts any
image the way the real BIOS handshake does), or RUNNING the multibooted flasher - which is the
real flasher code in ../../b2s/sim/sim, with a save chip that keeps its contents across power
cycles (cart_save file). When the image sent is GB-Link Cart Doctor's GBA cartridge program
(told apart by its size), the GBA plays that program instead (DumperEmu: info, ROM dump with its
data and fill records, save dump), with the game from SIM_ROM_FILE."""
import os, pty, struct, subprocess, threading, time, tty

SIM = os.path.join(os.path.dirname(os.path.abspath(__file__)), '../../b2s/sim/sim')
DUMPER = os.path.join(os.path.dirname(os.path.abspath(__file__)), '../assets/cartdoctor/gba-cart-dumper_mb.gba')
SAVE_SIZES = {'flash128': 0x20000, 'flash64': 0x10000, 'sram32': 0x8000, 'eeprom64k': 0x2000, 'eeprom4k': 0x200}
READY, DONE, MAGIC, GO, CONT, FAIL, ABORT = 0x52454459, 0x444F4E45, 0x47420000, 0x4742600D, 0x4742C047, 0x4742BAD0, 0x4742AB0B
FIN, SEEK = 0x4742F1A1, 0x53000000


class DumperEmu:
    """the GBA side of GB-Link Cart Doctor's GBA cartridge program: each exchange answers with
    the word it had ready, then takes the word it got"""
    def __init__(self, rom, save):
        self.rom, self.save = rom, save
        self.out, self.state, self.queue, self.cmd = READY, 'idle', [], 0
        rotl = lambda c: ((c << 1) | (c >> 31)) & 0xFFFFFFFF
        w = lambda o: struct.unpack_from('<I', rom, o)[0]
        self.stream, self.starts = [], {}
        n, s = len(rom) // 256, 0
        while s < n:
            self.starts[s] = len(self.stream)
            w0, run = w(s * 256), 0
            while s + run < n and rom[(s + run) * 256:(s + run + 1) * 256] == struct.pack('<I', w0) * 64:
                run += 1
            if run >= 2:
                h = 0xF1 << 24 | s
                self.stream += [h, w0, run, rotl(rotl(h) ^ w0) ^ run]
                s += run
                continue
            h = 0xDA << 24 | s
            c, words = h, [w(s * 256 + 4 * i) for i in range(64)]
            for x in words: c = rotl(c) ^ x
            self.stream += [h] + words + [c]
            s += 1
        self.stream.append(0xE0 << 24 | n)

    def _next(self):
        if self.queue: self.out = self.queue.pop(0)

    def _section(self):
        words = list(struct.unpack_from('<64I', self.save, self.pos * 256))
        x = 0
        for v in words: x ^= v
        self.queue = words + [x]
        self._next()

    def word(self, w):
        r = self.out
        st = self.state
        if st == 'idle':
            if w & 0xFFFF0000 == MAGIC and w != MAGIC:
                self.cmd, self.state, self.out = w & 0xFFFF, 'cmd', ~w & 0xFFFFFFFF
            else:
                self.out = READY
        elif st == 'cmd':
            if w != GO:
                self.state, self.out = 'idle', READY
                return r
            self.state, self.out = 'done', DONE
            hdr = list(struct.unpack_from('<48I', self.rom, 0))
            self.queue = {6: [len(self.rom), len(self.save)] + hdr, 1: [len(self.rom)], 2: [len(self.rom), len(self.save)]}.get(self.cmd, [])
        elif st == 'done':
            if w == CONT:
                if self.cmd == 2 and not self.queue:
                    self.state, self.pos = 'save', 0
                    self._section()
                else:
                    self.state = 'words'
                    self._next()
            else:
                self.out = DONE
        elif st == 'words':
            if self.queue:
                self._next()
            elif self.cmd == 1:
                self.state, self.pos = 'rom', 0
                self._rom()
            elif self.cmd == 2:
                self.state, self.out = 'done', DONE
            else:
                self.state, self.out = 'idle', READY
        elif st == 'rom':
            if w == FIN:
                self.state, self.out = 'idle', READY
            elif w & 0xFF000000 == SEEK:
                self.pos = self.starts[w & 0xFFFFFF]
                self._rom()
            elif w == ABORT:
                self.state, self.out = 'idle', READY
            else:
                self._rom()
        elif st == 'save':
            if self.queue:
                self._next()
            elif w == CONT:
                self.pos += 1
                if self.pos == len(self.save) // 256:
                    self.state, self.out = 'idle', READY
                else:
                    self._section()
            elif w == FAIL:
                self._section()
            elif w == ABORT:
                self.state, self.out = 'idle', READY
            # anything else: the last word of the section went out; wait for CONT or FAIL
        return r

    def _rom(self):
        self.out = self.stream[self.pos] if self.pos < len(self.stream) else 0
        self.pos += 1


class FakeGBLink:
    def __init__(self, cart_save, save_kind='flash128', log=lambda m: None):
        self.master, self.slave = pty.openpty()
        tty.setraw(self.slave)
        self.port = os.ttyname(self.slave)
        self.cart_save, self.save_kind, self.log = cart_save, save_kind, log
        self.state, self.sim, self.mb, self.dumper = 'off', None, None, None
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
        self.dumper = None
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
        if self.state == 'dumper':
            return self.dumper.word(w)
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
            if mb['fsize'] < (os.path.getsize(DUMPER) + os.path.getsize(os.path.join(os.path.dirname(DUMPER), '..', 'gblink_companion_mb.gba'))) // 2:   # Cart Doctor's program (the smaller image)
                rom = open(os.environ['SIM_ROM_FILE'], 'rb').read()
                save = open(self.cart_save, 'rb').read()[:SAVE_SIZES.get(self.save_kind, 0x20000)]
                self.dumper, self.state = DumperEmu(rom, save), 'dumper'
                self.multiboots += 1
                self.log('Cart Doctor\'s GBA program started')
            else:
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
