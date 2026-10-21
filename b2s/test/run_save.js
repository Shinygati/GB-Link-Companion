// Save backup/restore round trip against the simulator's save chip models.
// SIM_SAVE=<kind> [SIM_ROMSTR=...] node test/run_save.js <expectedDetectedType> <typeToUse> [misalign%]
const { spawn } = require('child_process');
const fs = require('fs'), path = require('path');
const B2S = require('../web/burn2slot.js');
const [expectType, useType, mis = '0'] = process.argv.slice(2).map(Number);
const dump = `/tmp/b2s_save_${process.pid}.bin`;
const sim = spawn(path.join(__dirname, '../sim/sim'), ['amd', '0', '0', '1', String(mis), dump, '7'], { stdio: ['pipe', 'pipe', 'inherit'] });
let buf = Buffer.alloc(0), waiter = null;
sim.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = { isNewFirmware: true, async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { sim.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); } };
const kind = process.env.SIM_SAVE;
const initial = n => Uint8Array.from({ length: n }, (_, i) => kind.startsWith('eeprom') ? (i * 11 + 5) & 0xFF : (i * 7 + (i >> 8) * 13) & 0xFF);
(async () => {
    const link = new B2S.Burn2SlotLink(usb, () => {});
    await link.configure();
    const info = await B2S.saveInfo(link);
    const size = B2S.SAVE_TYPES[useType].size;
    const before = await B2S.readSave(link, useType);
    const okRead = Buffer.from(before).equals(Buffer.from(initial(size)));
    const fresh = Uint8Array.from({ length: size }, (_, i) => (i * 2654435761 >>> 13) & 0xFF);
    await B2S.writeSave(link, useType, fresh);
    const after = await B2S.readSave(link, useType);
    sim.stdin.end();
    sim.on('exit', () => {
        const chip = fs.readFileSync(dump + '.sav');
        const okWrite = Buffer.from(after).equals(Buffer.from(fresh)) && chip.subarray(0, size).equals(Buffer.from(fresh));
        const ok = info.type === expectType && okRead && okWrite && info.title === 'SIMGAME TEST' && info.code === 'BSIM';
        console.log(`${ok ? 'PASS' : 'FAIL'} ${kind}: detected ${B2S.SAVE_TYPES[info.type] ? B2S.SAVE_TYPES[info.type].name : info.type}` +
            ` (flash id 0x${info.flashId.toString(16)}), "${info.title}" ${info.code}, backup ${okRead ? 'ok' : 'WRONG'}, restore ${okWrite ? 'ok' : 'WRONG'}`);
        fs.unlinkSync(dump); fs.unlinkSync(dump + '.sav');
        process.exit(ok ? 0 : 1);
    });
})().catch(e => { console.log('FAIL', kind, e.message); sim.kill(); process.exit(1); });
