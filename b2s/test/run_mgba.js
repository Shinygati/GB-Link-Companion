// Real GBA binary in mGBA + real host code: HELLO, DETECT, and full 64 KB WRITE packets.
// (mGBA's cart is read-only ROM, so DETECT finds no flash and WRITE answers "unknown flash" -
//  which it only does after the whole packet arrived with a correct checksum.)
const { spawn } = require('child_process');
const path = require('path');
const B2S = require('../web/burn2slot.js');
const gapUs = process.argv[2] || '60';
const emu = spawn(path.join(__dirname, 'mgba_link'), [path.join(__dirname, '../gba/burn2slot_mb.gba'), gapUs], { stdio: ['pipe', 'pipe', 'inherit', 'pipe'] });
let buf = Buffer.alloc(0), waiter = null;
emu.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = {
    isNewFirmware: true, async setMode() {}, async setTimingConfig(us) { if (process.env.FOLLOW) emu.stdio[3].write(us + '\n'); },
    async writeBytes(tx) { emu.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); },
};
(async () => {
    const link = new B2S.Burn2SlotLink(usb, m => console.log('  host:', m));
    if (process.env.START_GAP) link.gapUs = Number(process.env.START_GAP);
    await link.configure();
    const t0 = Date.now();
    const hello = await link.command(B2S.CMD.HELLO);
    console.log('HELLO', hello.status, '0x' + hello.data[0].toString(16));
    const det = await link.command(B2S.CMD.DETECT);
    console.log('DETECT status', det.status, '(1 = unknown flash, expected: mGBA cart is ROM)');
    let ok = hello.status === 0 && hello.data[0] === 0x10000 && det.status === 1;
    const block = new Uint32Array(0x4000); for (let i = 0; i < block.length; i++) block[i] = (i * 2654435761) >>> 0;
    for (let k = 0; k < 3; k++) {
        const w = await link.command(B2S.CMD.WRITE, [k * 0x10000, block.length, 0x30000], block);
        console.log(`WRITE 64KB #${k} status ${w.status} (1 = got the whole packet, checksum OK)`);
        ok = ok && w.status === 1;
    }
    const fin = await link.command(B2S.CMD.FINISH, [1]);
    ok = ok && fin.status === 0;
    console.log(ok ? 'PASS' : 'FAIL', `(${Date.now() - t0} ms)`);
    emu.stdin.end();
    emu.on('exit', () => process.exit(ok ? 0 : 1));
})().catch(e => { console.log('FAIL', e.message); emu.kill(); process.exit(1); });
