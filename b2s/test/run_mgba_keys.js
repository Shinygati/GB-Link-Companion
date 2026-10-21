// Controller mode on the real GBA binary in mGBA: KEYS + POLL returns the buttons held on the GBA,
// and normal commands still work straight afterwards.
const { spawn } = require('child_process');
const path = require('path');
const B2S = require('../web/burn2slot.js');
const W_KEYS = 0x4B455953, CMD_SHOW = 9, MSG_CONTROLLER = 1;
const keys = parseInt(process.argv[2] || '209', 16);      // A + Start + Down by default
const emu = spawn(path.join(__dirname, 'mgba_link'), [path.join(__dirname, '../gba/burn2slot_mb.gba'), '60'],
                  { stdio: ['pipe', 'pipe', 'inherit', 'pipe'], env: { ...process.env, MGBA_KEYS: keys.toString(16) } });
let buf = Buffer.alloc(0), waiter = null;
emu.stdout.on('data', d => { buf = Buffer.concat([buf, d]); if (waiter) { const w = waiter; waiter = null; w(); } });
const usb = {
    isNewFirmware: true, async setMode() {}, async setTimingConfig() {},
    async writeBytes(tx) { emu.stdin.write(Buffer.from(tx)); },
    async readBytesRaw(n) { while (!buf.length) await new Promise(r => (waiter = r)); const t = buf.subarray(0, Math.min(n, buf.length)); buf = buf.subarray(t.length); return new Uint8Array(t); },
};
(async () => {
    const link = new B2S.Burn2SlotLink(usb, () => {});
    await link.configure();
    let ok = await link.waitIdle();
    const show = await link.command(CMD_SHOW, [MSG_CONTROLLER], null, 5000, [0]);
    ok = ok && show.status === 0;
    let good = 0;
    for (let i = 0; i < 50; i++) {
        const r = await link.xfer([W_KEYS, B2S.W_POLL]);
        if (r[0] === B2S.W_IDLE && r[1] === ((0x4B000000 | keys) >>> 0)) good++;
        else console.log('  reply', r.map(x => x.toString(16)));
    }
    console.log(`KEYS polls answered correctly: ${good}/50 (keys 0x${keys.toString(16)})`);
    const after = await link.xfer([B2S.W_POLL]);
    const hello = await link.command(B2S.CMD.HELLO);
    ok = ok && good >= 49 && after[0] === B2S.W_IDLE && hello.status === 0 && hello.data[0] === 0x10000;
    console.log(ok ? 'PASS' : 'FAIL');
    emu.stdin.end();
    emu.on('exit', () => process.exit(ok ? 0 : 1));
})().catch(e => { console.log('FAIL', e.message); emu.kill(); process.exit(1); });
