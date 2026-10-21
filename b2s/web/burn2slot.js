/**
 * Burn2Slot for GB-Link - host side of the link protocol (see gba/proto.h).
 *
 * After the flasher has been multibooted, the GB-Link adapter stays in GB Link (SPI pass-through)
 * mode: every 4 bytes we send are clocked to the GBA as one 32-bit word, and we get back the word
 * the GBA had loaded. The GBA answers each word of a packet with ACK(k), checksums the packet,
 * does the work (detect / erase / write+verify) and then hands back a RESULT.
 *
 * Works with the UsbConnection / SerialConnection objects from gba-multiboot-web
 * (writeBytes, readBytesRaw, setMode, setTimingConfig, isNewFirmware).
 */
(function (root) {
    'use strict';

    const W_POLL = 0x504F4C4C, W_IDLE = 0x49444C45;
    const W_ACK = k => (0xA5000000 | (k & 0xFFFFFF)) >>> 0;
    const W_HEADER = (cmd, n) => (0x42000000 | (cmd << 16) | n) >>> 0;
    const CK_INIT = 0x12345678;
    const ckStep = (c, w) => ((((c << 5) | (c >>> 27)) >>> 0) + w) >>> 0;

    const CMD = { HELLO: 1, DETECT: 2, ERASE: 3, WRITE: 4, FINISH: 5, SAVE_INFO: 6, SAVE_READ: 7, SAVE_WRITE: 8 };
    const ST = { OK: 0, UNKNOWN_FLASH: 1, TIMEOUT: 2, CHECKSUM: 3, WRITE_FAIL: 4, VERIFY_FAIL: 5, BAD_COMMAND: 6, TOO_BIG: 7, NO_SAVE: 8 };
    const ST_NAME = ['OK', 'unknown flash chip', 'flash timed out', 'checksum error', 'write failed',
                     'verify failed', 'bad command', 'address out of range', 'no save type selected'];
    const RESULT_WORDS = { 1: [1], 2: [4], 3: [0], 4: [0, 1, 3], 5: [0], 6: [6], 8: [0, 3] };
    const SAVE_TYPES = {
        1: { name: 'SRAM / FRAM 32 KB', size: 0x8000 },  2: { name: 'SRAM 64 KB', size: 0x10000 },
        3: { name: 'Flash 64 KB', size: 0x10000 },        4: { name: 'Flash 128 KB', size: 0x20000 },
        5: { name: 'EEPROM 512 B', size: 0x200 },         6: { name: 'EEPROM 8 KB', size: 0x2000 },
    };
    const SAVE_CHUNK = 4096;
    const KIND = ['unknown', 'Intel', '22XX'];
    const MAKERS = { 0x52: 'Alliance', 0x1F: 'Atmel', 0x04: 'Fujitsu', 0x89: 'Intel', 0xC2: 'Macronix',
                     0x62: 'Sanyo', 0xB0: 'Sharp', 0x01: 'Spansion', 0xBF: 'SST', 0x20: 'ST',
                     0xDA: 'Winbond', 0x1C: 'Mitsubishi' };

    const BLOCK_BYTES = 0x4000;      // one WRITE packet (the GBA accepts up to 64 KB)
    const ERASE_STEP_WORDS = 0x8000; // Burn2Slot erases in 0x8000-word (64 KB) steps
    const BATCH_WORDS = 15;          // 60 bytes per USB transfer (below the 64-byte packet size)
    const GAP_US = 60;               // time between words (the GBA re-arms in this gap); widened on errors
    const GAP_MAX_US = 300;

    const delay = ms => new Promise(r => setTimeout(r, ms));
    const hex = (v, n = 8) => '0x' + (v >>> 0).toString(16).toUpperCase().padStart(n, '0');

    class LinkError extends Error {}
    class RetryError extends Error {}

    class Burn2SlotLink {
        constructor(usb, log = () => {}) {
            this.usb = usb;
            this.log = log;
            this.cancelled = false;
            this.gapUs = GAP_US;
        }

        async configure() {
            if (this.usb.isNewFirmware && this.usb.setMode) {
                await this.usb.setMode(2 /* MODE.GB_LINK */);
                await delay(100);
            }
            await this.usb.setTimingConfig(this.gapUs, 4);
        }

        /** The link slipped: give the GBA more time between words from now on. */
        async widenGap() {
            if (this.gapUs >= GAP_MAX_US) return;
            this.gapUs = Math.min(GAP_MAX_US, this.gapUs + 40);
            await this.usb.setTimingConfig(this.gapUs, 4);
            this.log(`Slowing the link down a little (${this.gapUs} µs between words).`, 'info');
        }

        /** Exchange words with the GBA; returns the GBA's reply to each. */
        async xfer(words) {
            const out = [];
            for (let i = 0; i < words.length; i += BATCH_WORDS) {
                if (this.cancelled) throw new LinkError('Cancelled');
                const batch = words.slice(i, i + BATCH_WORDS);
                const tx = new Uint8Array(batch.length * 4);
                batch.forEach((w, j) => {
                    tx[j * 4] = w >>> 24; tx[j * 4 + 1] = (w >>> 16) & 0xFF;
                    tx[j * 4 + 2] = (w >>> 8) & 0xFF; tx[j * 4 + 3] = w & 0xFF;
                });
                await this.usb.writeBytes(tx);
                const rx = new Uint8Array(tx.length);
                let got = 0;
                while (got < rx.length) {
                    const chunk = await this.usb.readBytesRaw(rx.length - got, 2000);
                    if (!chunk || chunk.length === 0) throw new LinkError('No response from the GB-Link adapter');
                    rx.set(chunk.subarray ? chunk.subarray(0, rx.length - got) : chunk.slice(0, rx.length - got), got);
                    got += chunk.length;
                }
                for (let j = 0; j < batch.length; j++)
                    out.push(((rx[j * 4] << 24) | (rx[j * 4 + 1] << 16) | (rx[j * 4 + 2] << 8) | rx[j * 4 + 3]) >>> 0);
            }
            return out;
        }

        /** Poll until the GBA is listening for a new packet. */
        async waitIdle(timeoutMs = 5000) {
            const end = Date.now() + timeoutMs;
            while (Date.now() < end) {
                const r = await this.xfer([W_POLL, W_POLL, W_POLL, W_POLL]);
                if (r[r.length - 1] === W_IDLE) return true;
                await delay(5);
            }
            return false;
        }

        async sendPacket(cmd, args, data) {
            const words = [W_HEADER(cmd, args.length), ...args.map(a => a >>> 0)];
            if (data) for (let i = 0; i < data.length; i++) words.push(data[i]);
            let ck = CK_INIT;
            for (const w of words) ck = ckStep(ck, w);
            words.push(ck);

            for (let i = 0; i < words.length; i += BATCH_WORDS * 8) {
                const part = words.slice(i, i + BATCH_WORDS * 8);
                const replies = await this.xfer(part);
                for (let j = 0; j < replies.length; j++) {
                    const k = i + j, want = k === 0 ? W_IDLE : W_ACK(k);
                    if (replies[j] !== want) {
                        throw new RetryError(`link out of step at word ${k} (got ${hex(replies[j])})`);
                    }
                }
            }
        }

        async getResult(cmd, timeoutMs, counts) {
            counts = counts || RESULT_WORDS[cmd];
            const end = Date.now() + timeoutMs;
            let pending = [], need = BATCH_WORDS;
            while (Date.now() < end) {
                pending.push(...await this.xfer(new Array(need).fill(W_POLL)));
                need = BATCH_WORDS;
                // look for RESULT, its data words and the IDLE that follows
                let waiting = false;
                for (let i = 0; i < pending.length; i++) {
                    const w = pending[i];
                    if ((w >>> 24) !== 0x52) continue;
                    const status = (w >>> 16) & 0xFF, count = w & 0xFFFF;
                    if (status >= ST_NAME.length || !counts.includes(count)) continue;
                    if (i + count + 1 >= pending.length) {          // result still arriving
                        pending = pending.slice(i);
                        need = Math.max(BATCH_WORDS, Math.min(BATCH_WORDS * 16, count + 2 - pending.length));
                        waiting = true;
                        break;
                    }
                    if (pending[i + count + 1] !== W_IDLE) {
                        // A word went missing (or one too many) on the way: the GBA has already
                        // finished and is idle. Ask for the result again by resending the command.
                        const after = pending.slice(i + count + 1);
                        if (after.includes(W_IDLE)) throw new RetryError('result garbled');
                        if (after.length < 64) { pending = pending.slice(i); waiting = true; break; }  // keep it until the IDLE shows
                        continue;
                    }
                    return { status, data: pending.slice(i + 1, i + 1 + count) };
                }
                if (!waiting && pending.length > 64) pending = pending.slice(-32);
            }
            throw new LinkError('The GBA stopped responding');
        }

        /** Send one command, with retries if the link gets out of step or the data was corrupted. */
        async command(cmd, args = [], data = null, timeoutMs = 10000, counts = null) {
            let lastErr = null;
            for (let attempt = 0; attempt < 8; attempt++) {
                if (this.cancelled) throw new LinkError('Cancelled');
                if (attempt) await delay(400);       // let the GBA give up on the half-sent packet
                if (!(await this.waitIdle(attempt ? 3000 : 8000))) { lastErr = new LinkError('The GBA is not responding'); continue; }
                try {
                    await this.sendPacket(cmd, args, data);
                } catch (e) {
                    if (!(e instanceof RetryError)) throw e;
                    lastErr = e; this.log(`Link hiccup (${e.message}), retrying...`, 'info');
                    if (attempt >= 1) await this.widenGap();
                    continue;
                }
                let res;
                try {
                    res = await this.getResult(cmd, timeoutMs, counts);
                } catch (e) {
                    if (!(e instanceof RetryError)) throw e;
                    lastErr = e; this.log('The reply was garbled, asking again...', 'info');
                    continue;
                }
                if (res.status === ST.CHECKSUM) { lastErr = new RetryError('checksum error'); this.log('Data was corrupted on the way, resending...', 'info'); continue; }
                return res;
            }
            throw lastErr || new LinkError('Command failed');
        }
    }

    /**
     * Flash `rom` (Uint8Array) to the bootleg cart. Assumes the flasher is already running on the GBA.
     * onProgress(phase, done, total)
     */
    async function flashRom(usb, rom, { log = () => {}, onProgress = () => {}, link = null } = {}) {
        link = link || new Burn2SlotLink(usb, log);
        await link.configure();

        log('Connecting to the flasher on the GBA...');
        const hello = await link.command(CMD.HELLO);
        log(`Flasher version ${hello.data[0] >>> 16}.${(hello.data[0] >>> 8) & 0xFF}.${hello.data[0] & 0xFF}`, 'success');

        log('Detecting the flash chip...');
        const det = await link.command(CMD.DETECT, [], null, 20000);
        const [flashid, maker, size, extra] = det.data;
        const info = {
            flashid, maker, size, bufferSize: extra & 0xFFFF, busType: (extra >>> 16) & 0xFF, seqType: extra >>> 24,
            makerName: MAKERS[maker & 0xFF] || 'Unknown',
        };
        if (det.status !== ST.OK) throw new LinkError(`Unknown flash chip (ID ${hex(flashid, 4)}). This cart isn't supported by Burn2Slot.`);
        log(`Flash ID ${hex(flashid, 4)}, maker ${info.makerName}, size ${size ? (size / 1048576) + ' MB' : 'unknown'}`, 'success');
        if (size && rom.length > size) throw new LinkError(`The ROM (${(rom.length / 1048576).toFixed(1)} MB) is bigger than the cart (${size / 1048576} MB).`);
        if (rom.length > 0x2000000) throw new LinkError('ROMs larger than 32 MB are not supported.');

        // pad to a multiple of 4 bytes with 0xFF (erased flash)
        const len = (rom.length + 3) & ~3;
        const data = new Uint8Array(len).fill(0xFF);
        data.set(rom);
        const words = new Uint32Array(data.buffer, 0, len / 4);

        // erase(fileSize >> 1): every 0x8000-word step that the ROM touches
        const eraseWords = Math.ceil(len / 2 / ERASE_STEP_WORDS) * ERASE_STEP_WORDS;
        log(`Erasing ${(eraseWords * 2 / 1048576).toFixed(2)} MB...`);
        for (let addr = 0; addr < eraseWords; addr += ERASE_STEP_WORDS) {
            const r = await link.command(CMD.ERASE, [addr, eraseWords], null, 30000);
            if (r.status !== ST.OK) throw new LinkError(`Erase failed at ${hex(addr * 2)}: ${ST_NAME[r.status]}`);
            onProgress('erase', addr + ERASE_STEP_WORDS, eraseWords);
        }

        log('Writing...');
        const t0 = Date.now();
        let skipped = 0;
        for (let off = 0; off < len; off += BLOCK_BYTES) {
            const n = Math.min(BLOCK_BYTES, len - off) / 4;
            const block = words.subarray(off / 4, off / 4 + n);
            if (block.every(w => w === 0xFFFFFFFF)) { skipped++; onProgress('write', off + n * 4, len); continue; }
            let r;
            for (let tries = 0; ; tries++) {
                r = await link.command(CMD.WRITE, [off, n, len], block, 30000);
                if (r.status === ST.OK || tries >= 1 || (r.status !== ST.WRITE_FAIL && r.status !== ST.VERIFY_FAIL)) break;
                log(`Block at ${hex(off)} didn't verify, writing it again...`, 'info');
            }
            if (r.status === ST.WRITE_FAIL) throw new LinkError(`Write failed at ${hex(r.data[0])}`);
            if (r.status === ST.VERIFY_FAIL)
                throw new LinkError(`Verify failed at ${hex(r.data[0])}: read ${hex(r.data[1], 4)}, expected ${hex(r.data[2], 4)}`);
            if (r.status !== ST.OK) throw new LinkError(`Write failed at ${hex(off)}: ${ST_NAME[r.status]}`);
            onProgress('write', off + n * 4, len);
        }
        const secs = (Date.now() - t0) / 1000;
        await link.command(CMD.FINISH, [1]);
        log(`Done! Wrote and verified ${(len / 1048576).toFixed(2)} MB in ${secs.toFixed(0)} s` +
            (skipped ? ` (${skipped} empty 16 KB blocks skipped)` : '') + '.', 'success');
        return info;
    }

    /** What save chip does the cart in the GBA have? */
    async function saveInfo(link) {
        const r = await link.command(CMD.SAVE_INFO, [], null, 60000);
        const bytes = new Uint8Array(new Uint32Array(r.data.slice(2, 6)).buffer);
        const txt = b => Array.from(b, c => (c >= 32 && c < 127 ? String.fromCharCode(c) : ' ')).join('').trim();
        return { type: r.data[0], flashId: r.data[1], title: txt(bytes.subarray(0, 12)), code: txt(bytes.subarray(12, 16)) };
    }

    /** Read the whole save of `type` and return it as a Uint8Array. */
    async function readSave(link, type, onProgress = () => {}) {
        const size = SAVE_TYPES[type].size, out = new Uint8Array(size);
        for (let off = 0; off < size; off += SAVE_CHUNK) {
            const n = Math.min(SAVE_CHUNK, size - off);
            let data = null;
            for (let tries = 0; tries < 4 && !data; tries++) {
                const r = await link.command(CMD.SAVE_READ, [type, off, n], null, 20000, [0, n / 4 + 1]);
                if (r.status !== ST.OK) throw new LinkError(`Reading the save failed: ${ST_NAME[r.status]}`);
                let ck = CK_INIT;
                for (let i = 1; i < r.data.length; i++) ck = ckStep(ck, r.data[i]);
                if (ck === r.data[0]) data = r.data.slice(1);
                else link.log('Save data was corrupted on the way, reading it again...', 'info');
            }
            if (!data) throw new LinkError('The save data kept arriving corrupted');
            out.set(new Uint8Array(new Uint32Array(data).buffer), off);
            onProgress(off + n, size);
        }
        return out;
    }

    /** Write `data` (exactly the save size) to the save chip and verify it. */
    async function writeSave(link, type, data, onProgress = () => {}) {
        const size = SAVE_TYPES[type].size;
        if (data.length !== size) throw new LinkError(`The save file is ${data.length} bytes, but a ${SAVE_TYPES[type].name} save is ${size} bytes.`);
        const words = new Uint32Array(data.slice().buffer);
        for (let off = 0; off < size; off += SAVE_CHUNK) {
            const n = Math.min(SAVE_CHUNK, size - off);
            const r = await link.command(CMD.SAVE_WRITE, [type, off, n], words.subarray(off / 4, (off + n) / 4), 60000);
            if (r.status === ST.VERIFY_FAIL)
                throw new LinkError(`The save didn't verify at ${hex(r.data[0], 5)}: read ${hex(r.data[1], 2)}, expected ${hex(r.data[2], 2)}`);
            if (r.status !== ST.OK) throw new LinkError(`Writing the save failed: ${ST_NAME[r.status]}`);
            onProgress(off + n, size);
        }
    }

    async function reportFailure(usb, log) {
        try {
            const link = new Burn2SlotLink(usb, log);
            await link.command(CMD.FINISH, [0], null, 3000);
        } catch (e) { /* GBA may be gone */ }
    }

    const api = { flashRom, reportFailure, saveInfo, readSave, writeSave, SAVE_TYPES, Burn2SlotLink, LinkError, CMD, ST, KIND,
                  W_POLL, W_IDLE, W_ACK, W_HEADER, ckStep, CK_INIT };
    if (typeof module !== 'undefined' && module.exports) module.exports = api;
    root.Burn2Slot = api;
})(typeof window !== 'undefined' ? window : globalThis);
