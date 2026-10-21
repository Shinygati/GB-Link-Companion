// Screens of GB-Link Companion's helper (gba/gblink_companion_mb.gba) in mGBA, saved as PPM files:
// after the save is loaded, half-way through writing changed blocks, after writing, controller mode.
const { spawn } = require('child_process');
const path = require('path');
const B2S = require('../web/burn2slot.js');
const out = process.argv[2] || '.';
const bin = process.argv[3] || path.join(__dirname, '../gba/gblink_companion_mb.gba');
const emu = spawn(path.join(__dirname, 'mgba_link'), [bin, '60'], { stdio: ['pipe', 'pipe', 'inherit', 'pipe'] });
let buf = Buffer.alloc(0), waiter = null;
emu.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = {
    isNewFirmware: true, async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { emu.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); },
};
const shot = async name => { emu.stdio[3].write('s' + path.join(out, name) + '\n'); await new Promise(r => setTimeout(r, 2500)); };
(async () => {
    const link = new B2S.Burn2SlotLink(usb, () => {});
    await link.configure();
    await shot('0_boot.ppm');
    await link.command(B2S.CMD.HELLO);
    await link.command(6, [], null, 20000, [6]);                       // SAVE_INFO
    for (let k = 0; k < 8; k++) await link.command(7, [1, k * 4096, 4096, k, 8], null, 20000, [0, 1025]);
    await shot('1_loaded.ppm');
    const data = new Uint32Array(1024);
    await link.command(8, [1, 0x3000, 4096, 0, 3], data, 20000, [0, 3]);  // 3 changed blocks
    await shot('2_writing.ppm');
    await link.command(8, [1, 0x5000, 4096, 1, 3], data, 20000, [0, 3]);
    await link.command(8, [1, 0x7000, 4096, 2, 3], data, 20000, [0, 3]);
    await shot('3_written.ppm');
    await link.command(9, [1], null, 5000, [0]);                       // controller mode
    await shot('4_controller.ppm');
    emu.stdin.end();
    emu.on('exit', () => process.exit(0));
})().catch(e => { console.log('FAIL', e.message); emu.kill(); process.exit(1); });
