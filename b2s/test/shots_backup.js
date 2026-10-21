// The GBA's screens during a cartridge backup (gba/gblink_companion_mb.gba in mGBA, with a test cart),
// saved as PPM files: copying, cancelled, copying the save, finished.
//   node test/shots_backup.js OUTDIR
const { spawn } = require('child_process');
const fs = require('fs'), os = require('os'), path = require('path');
const B2S = require('../web/burn2slot.js');
const out = process.argv[2] || '.';
const romPath = path.join(os.tmpdir(), 'b2s_shot_cart.gba');
const rom = Buffer.alloc(2 << 20, 0x5A);
rom.writeUInt32LE(0x51AEFF24, 4); rom.write('POKEMON RUBY', 0xA0, 'latin1'); rom.write('AXVE', 0xAC, 'latin1');
fs.writeFileSync(romPath, rom);
const emu = spawn(path.join(__dirname, 'mgba_link'), [path.join(__dirname, '../gba/gblink_companion_mb.gba'), '60'],
                  { stdio: ['pipe', 'pipe', 'ignore', 'pipe'], env: { ...process.env, MGBA_CART: romPath } });
let buf = Buffer.alloc(0), waiter = null;
emu.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = { isNewFirmware: true, async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { emu.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); } };
const shot = async name => { emu.stdio[3].write('s' + path.join(out, name) + '\n'); await new Promise(r => setTimeout(r, 1500)); };
(async () => {
    const link = new B2S.Burn2SlotLink(usb, () => {});
    await link.configure();
    await link.command(B2S.CMD.HELLO);
    await link.command(10, [], null, 30000, [49]);                          // ROM_INFO
    for (let k = 0; k < 20; k++) await link.command(11, [k * 0x8000, 0x8000, k, 64], null, 20000, [0x2001]);
    await shot('1_copying.ppm');
    await link.command(9, [2], null, 5000, [0]);                            // cancelled
    await shot('2_stopped.ppm');
    await link.command(9, [3], null, 5000, [0]);                            // a backup's save next
    for (let k = 0; k < 8; k++) await link.command(7, [1, k * 4096, 4096, k, 8], null, 20000, [0, 1025]);
    await shot('3_save_copied.ppm');
    await link.command(9, [4], null, 5000, [0]);                            // finished
    await shot('4_finished.ppm');
    console.log('done'); emu.stdin.end(); emu.on('exit', () => process.exit(0));
})().catch(e => { console.log('FAIL', e.message); emu.kill(); process.exit(1); });
