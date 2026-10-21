// Real GBA binary in mGBA: detect the (emulated) save chip, restore a save, back it up, compare.
const { spawn } = require('child_process');
const path = require('path');
const B2S = require('../web/burn2slot.js');
const emu = spawn(path.join(__dirname, 'mgba_link'), [path.join(__dirname, '../gba/burn2slot_mb.gba'), '60'], { stdio: ['pipe', 'pipe', 'inherit', 'pipe'] });
let buf = Buffer.alloc(0), waiter = null;
emu.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = { isNewFirmware: true, async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { emu.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); } };
(async () => {
    const link = new B2S.Burn2SlotLink(usb, () => {});
    await link.configure();
    const info = await B2S.saveInfo(link);
    const type = info.type;
    console.log(`detected: ${B2S.SAVE_TYPES[type].name}, flash id 0x${info.flashId.toString(16)}`);
    const size = B2S.SAVE_TYPES[type].size;
    const data = Uint8Array.from({ length: size }, (_, i) => (i * 2654435761 >>> 11) & 0xFF);
    await B2S.writeSave(link, type, data);
    const back = await B2S.readSave(link, type);
    const ok = Buffer.from(back).equals(Buffer.from(data));
    console.log(ok ? 'PASS' : 'FAIL', 'save restore + backup round trip');
    emu.stdin.end(); emu.on('exit', () => process.exit(ok ? 0 : 1));
})().catch(e => { console.log('FAIL', e.message); emu.kill(); process.exit(1); });
