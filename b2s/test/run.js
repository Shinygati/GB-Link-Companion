// Drives the real web host code (web/burn2slot.js) against the native GBA simulator.
// node test/run.js <amd|intel> <bus> <seq> <chipMB> <romBytes> <misalign%> [seed]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const B2S = require('../web/burn2slot.js');

const [kind, bus, seq, chipMB, romBytes, mis, seed = '1'] = process.argv.slice(2);
const dump = `/tmp/b2s_dump_${process.pid}.bin`;
const sim = spawn(path.join(__dirname, '../sim/sim'), [kind, bus, seq, chipMB, mis, dump, seed], { stdio: ['pipe', 'pipe', 'inherit'] });

let buf = Buffer.alloc(0), waiter = null;
sim.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });

const usb = {
    isNewFirmware: true,
    async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { sim.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) {
        while (buf.length < 1) await new Promise(r => (waiter = r));
        const take = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(take.length);
        return new Uint8Array(take);
    },
};

// ROM: random data with some 0xFF padding (to exercise skipped blocks) and an odd length
let rnd = Number(seed) * 2654435761 >>> 0;
const rom = new Uint8Array(Number(romBytes));
for (let i = 0; i < rom.length; i++) { rnd = (rnd * 1103515245 + 12345) >>> 0; rom[i] = rnd >>> 24; }
if (rom.length > 0x30000) rom.fill(0xFF, 0x10000, 0x30000);

const t0 = Date.now();
B2S.flashRom(usb, rom, { log: (m, t) => process.env.QUIET || console.log('  host:', m) })
    .then(info => {
        sim.stdin.end();
        sim.on('exit', () => {
            const got = fs.readFileSync(dump);
            const padded = Buffer.alloc((rom.length + 3) & ~3, 0xFF); padded.set(rom);
            const ok = got.subarray(0, padded.length).equals(padded);
            let bad = -1; if (!ok) for (let i = 0; i < padded.length; i++) if (got[i] !== padded[i]) { bad = i; break; }
            console.log(`${ok ? 'PASS' : 'FAIL'} ${kind} bus=${bus} seq=${seq} rom=${romBytes} misalign=${mis}% ` +
                        `id=0x${info.flashid.toString(16)} size=${info.size} (${Date.now() - t0} ms)` + (ok ? '' : ` first diff at 0x${bad.toString(16)}`));
            fs.unlinkSync(dump);
            process.exit(ok ? 0 : 1);
        });
    })
    .catch(e => { console.log(`FAIL ${kind} bus=${bus} seq=${seq}: ${e.message}`); sim.kill(); process.exit(1); });
