// ROM dump on the real GBA binary in mGBA: ROM_INFO finds the cart's size from open bus, and
// ROM_READ returns the cart's contents with a correct checksum.
//   node test/run_mgba_rom.js [helper.gba]
const { spawn } = require('child_process');
const fs = require('fs'), os = require('os'), path = require('path');
const B2S = require('../web/burn2slot.js');
const CMD_ROM_INFO = 10, CMD_ROM_READ = 11, CHUNK = Number(process.env.CHUNK || 0x8000);
const romPath = path.join(os.tmpdir(), 'b2s_test_cart.gba');
const rom = Buffer.alloc(Number(process.env.ROMMB || 2) << 20);
for (let i = 0; i < rom.length; i++) rom[i] = (i * 2654435761 >>> 13) & 0xFF;
rom.writeUInt32LE(0x51AEFF24, 4);                         // start of the Nintendo logo
rom.write('DUMPTEST    ', 0xA0, 'latin1'); rom.write('BDMP', 0xAC, 'latin1');
fs.writeFileSync(romPath, rom);
const bin = process.argv[2] || path.join(__dirname, '../gba/gblink_companion_mb.gba');
// SIM=1: run the helper's C code natively in the simulator instead (link loss and misaligned
// re-syncs included), with the same ROM in its cart.
const emu = process.env.SIM
    ? spawn(path.join(__dirname, '../sim/sim'), ['amd', '0', '0', String(process.env.ROMMB || 2), process.env.SIM, '/tmp/b2s_rom_sim.bin', '5'], { stdio: ['pipe', 'pipe', 'inherit'], env: { ...process.env, SIM_ROM_FILE: romPath } })
    : spawn(path.join(__dirname, 'mgba_link'), [bin, '60'], { stdio: ['pipe', 'pipe', process.env.SHOWERR ? 'inherit' : 'ignore', 'pipe'], env: { ...process.env, ...(process.env.NOCART ? {} : { MGBA_CART: romPath }) } });
let buf = Buffer.alloc(0), waiter = null;
emu.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = {
    isNewFirmware: true, async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { emu.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); },
};
(async () => {
    const link = new B2S.Burn2SlotLink(usb, m => process.env.SHOWERR && console.log('  host:', m));
    await link.configure();
    const info = await link.command(CMD_ROM_INFO, [], null, 30000, [49]);
    const size = process.env.NOCART ? rom.length : info.data[0];
    const title = Buffer.from(Uint32Array.from(info.data.slice(1)).buffer).subarray(0xA0, 0xB0).toString('latin1');
    console.log(`ROM_INFO status ${info.status}, size ${size / 1048576} MB, header "${title}"`);
    let ok = info.status === 0 && size === rom.length && title === 'DUMPTEST    BDMP';
    const t0 = Date.now(), steps = size / CHUNK;
    const out = Buffer.alloc(size);
    let retries = 0;
    for (let k = 0; k < steps; k++) {
        let r;
        for (let t = 0; ; t++) {          // like the app: a chunk that doesn't arrive is asked for again
            try { r = await link.command(CMD_ROM_READ, (process.env.NOSTEPS ? [k * CHUNK, CHUNK] : [k * CHUNK, CHUNK, k, steps]), null, 8000, [0, CHUNK / 4 + 1]); break; }
            catch (e) { if (t >= 3) throw e; retries++; await link.waitIdle(10000); }   // let the GBA give up on the old reply
        }
        let ck = B2S.CK_INIT;
        for (const w of r.data.slice(1)) ck = B2S.ckStep(ck, w);
        if (r.status !== 0 || ck !== r.data[0]) { console.log('bad chunk', k, 'status', r.status, r.data.length); ok = false; break; }
        Buffer.from(Uint32Array.from(r.data.slice(1)).buffer).copy(out, k * CHUNK);
    }
    const same = out.equals(rom);
    console.log(`retries: ${retries}`);
    console.log(`read ${size >> 20} MB in ${((Date.now() - t0) / 1000).toFixed(1)} s (emulated), matches the cart: ${same}`);
    ok = ok && same;
    console.log(ok ? 'PASS' : 'FAIL');
    emu.stdin.end();
    emu.on('exit', () => process.exit(ok ? 0 : 1));
})().catch(e => { console.log('FAIL', e.message); emu.stdin.end(); setTimeout(() => process.exit(1), 4000); });
