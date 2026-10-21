/**
 * Burn2Slot for GB-Link - page logic.
 * 1. connect the adapter, 2. multiboot the flasher (burn2slot_mb.js) and detect the chip,
 * 3. stream the chosen ROM to the flasher (burn2slot.js).
 */
class App {
    constructor() {
        this.usb = null;
        this.romData = null;
        this.romName = null;
        this.mbData = null;
        this.busy = false;
        this.link = null;
        const $ = id => document.getElementById(id);
        this.el = {
            dropZone: $('drop-zone'), fileInput: $('file-input'), fileInfo: $('file-info'),
            fileName: $('file-name'), fileSize: $('file-size'), btnClearFile: $('btn-clear-file'),
            btnConnect: $('btn-connect'), usbStatus: $('usb-status'),
            btnMultiboot: $('btn-multiboot'), btnDetect: $('btn-detect'), chipInfo: $('chip-info'),
            btnFlash: $('btn-flash'), btnCancel: $('btn-cancel'),
            progressWrap: $('progress-wrap'), progressPhase: $('progress-phase'),
            progressText: $('progress-text'), progressFill: $('progress-fill'),
            mbDropZone: $('mb-drop-zone'), mbFileInput: $('mb-file-input'), mbFileInfo: $('mb-file-info'),
            mbFileName: $('mb-file-name'), mbFileSize: $('mb-file-size'), mbBtnClear: $('mb-btn-clear'),
            btnSendMb: $('btn-send-mb'),
            btnSaveDetect: $('btn-save-detect'), saveType: $('save-type'), saveInfo: $('save-info'),
            btnSaveBackup: $('btn-save-backup'), btnSaveRestore: $('btn-save-restore'), saveFileInput: $('save-file-input'),
            saveProgressWrap: $('save-progress-wrap'), saveProgressPhase: $('save-progress-phase'),
            saveProgressText: $('save-progress-text'), saveProgressFill: $('save-progress-fill'),
            btnDarkMode: $('btn-dark-mode'), log: $('log-container'), browserWarning: $('browser-warning'),
        };
        this.init();
    }

    init() {
        const hasUsb = 'usb' in navigator, hasSerial = 'serial' in navigator;
        this.transport = hasUsb ? 'usb' : (hasSerial ? 'serial' : null);
        if (!this.transport) {
            this.el.browserWarning.classList.add('show');
            this.el.btnConnect.disabled = true;
            this.log('Neither WebUSB nor WebSerial is supported in this browser.', 'error');
            return;
        }
        this.el.btnConnect.textContent = this.transport === 'serial' ? 'Connect Serial' : 'Connect USB';

        const dz = this.el.dropZone;
        dz.addEventListener('click', () => this.el.fileInput.click());
        dz.addEventListener('dragover', e => { e.preventDefault(); dz.classList.add('drag-over'); });
        dz.addEventListener('dragleave', () => dz.classList.remove('drag-over'));
        dz.addEventListener('drop', e => {
            e.preventDefault(); dz.classList.remove('drag-over');
            if (e.dataTransfer.files[0]) this.loadFile(e.dataTransfer.files[0]);
        });
        this.el.fileInput.addEventListener('change', e => e.target.files[0] && this.loadFile(e.target.files[0]));
        this.el.btnClearFile.addEventListener('click', () => this.clearFile());
        this.el.btnConnect.addEventListener('click', () => this.connect());
        this.el.btnMultiboot.addEventListener('click', () => this.sendFlasher());
        this.el.btnDetect.addEventListener('click', () => this.detect());
        this.el.btnFlash.addEventListener('click', () => this.flash());
        this.el.btnCancel.addEventListener('click', () => { if (this.link) this.link.cancelled = true; });
        const mz = this.el.mbDropZone;
        mz.addEventListener('click', () => this.el.mbFileInput.click());
        mz.addEventListener('dragover', e => { e.preventDefault(); mz.classList.add('drag-over'); });
        mz.addEventListener('dragleave', () => mz.classList.remove('drag-over'));
        mz.addEventListener('drop', e => {
            e.preventDefault(); mz.classList.remove('drag-over');
            if (e.dataTransfer.files[0]) this.loadMbFile(e.dataTransfer.files[0]);
        });
        this.el.mbFileInput.addEventListener('change', e => e.target.files[0] && this.loadMbFile(e.target.files[0]));
        this.el.mbBtnClear.addEventListener('click', () => this.clearMbFile());
        this.el.btnSendMb.addEventListener('click', () => this.sendMultiboot());
        this.el.btnSaveDetect.addEventListener('click', () => this.detectSave());
        this.el.btnSaveBackup.addEventListener('click', () => this.backupSave());
        this.el.btnSaveRestore.addEventListener('click', () => this.el.saveFileInput.click());
        this.el.saveFileInput.addEventListener('change', e => { const f = e.target.files[0]; e.target.value = ''; if (f) this.restoreSave(f); });
        this.el.saveType.addEventListener('change', () => this.updateButtons());
        this.el.btnDarkMode.addEventListener('click', () => {
            const light = document.body.classList.toggle('light-mode');
            this.el.btnDarkMode.textContent = light ? '☀️' : '🌙';
        });
        window.addEventListener('beforeunload', e => { if (this.busy) { e.preventDefault(); e.returnValue = ''; } });
        this.log('Ready. Connect your GB-Link adapter to begin.');
    }

    async loadFile(file) {
        if (!/\.(gba|bin)$/i.test(file.name)) { this.log('Please choose a .gba file', 'error'); return; }
        this.romData = new Uint8Array(await file.arrayBuffer());
        this.romName = file.name;
        this.el.fileName.textContent = file.name;
        this.el.fileSize.textContent = this.formatSize(file.size);
        this.el.dropZone.style.display = 'none';
        this.el.fileInfo.style.display = 'flex';
        const title = new TextDecoder('ascii').decode(this.romData.subarray(0xA0, 0xAC)).replace(/\0.*$/, '');
        const code = new TextDecoder('ascii').decode(this.romData.subarray(0xAC, 0xB0));
        this.log(`Loaded ${file.name} (${this.formatSize(file.size)})` + (title ? ` - ${title} [${code}]` : ''), 'success');
        this.updateButtons();
    }

    clearFile() {
        this.romData = null; this.romName = null;
        this.el.fileInput.value = '';
        this.el.dropZone.style.display = 'block';
        this.el.fileInfo.style.display = 'none';
        this.updateButtons();
    }

    async loadMbFile(file) {
        if (!/\.(gba|bin|mb)$/i.test(file.name)) { this.log('Please choose a .gba, .bin or .mb file', 'error'); return; }
        if (file.size > 0x40000) { this.log(`${file.name} is too big for multiboot (max 256 KB).`, 'error'); return; }
        this.mbData = await file.arrayBuffer();
        this.mbName = file.name;
        this.el.mbFileName.textContent = file.name;
        this.el.mbFileSize.textContent = this.formatSize(file.size);
        this.el.mbDropZone.style.display = 'none';
        this.el.mbFileInfo.style.display = 'flex';
        this.log(`Loaded multiboot file ${file.name} (${this.formatSize(file.size)})`, 'success');
        this.updateButtons();
    }

    clearMbFile() {
        this.mbData = null; this.mbName = null;
        this.el.mbFileInput.value = '';
        this.el.mbDropZone.style.display = 'block';
        this.el.mbFileInfo.style.display = 'none';
        this.updateButtons();
    }

    async sendMultiboot() {
        await this.run(async () => {
            this.log(`Sending ${this.mbName}. Now turn the GBA on (no cart, or hold START + SELECT)...`);
            const ok = await window.GBAMultiboot.multiboot(this.usb, this.mbData, (m, t) => this.log(m, t));
            if (!ok) this.log('Multiboot failed. Turn the GBA off and try again.', 'error');
        });
    }

    // ---------------------------------------------------------------- save backup / restore
    newLink() {
        this.link = new Burn2Slot.Burn2SlotLink(this.usb, (m, t) => this.log(m, t));
        return this.link;
    }

    chosenSaveType() {
        const v = Number(this.el.saveType.value);
        return v || (this.save ? this.save.type : 0);
    }

    async detectSave() {
        await this.run(async () => {
            const link = this.newLink();
            await link.configure();
            this.log('Checking the save chip...');
            const info = await Burn2Slot.saveInfo(link);
            this.save = info;
            const t = Burn2Slot.SAVE_TYPES[info.type];
            const hex = v => v.toString(16).toUpperCase().padStart(4, '0');
            this.el.saveInfo.textContent = `${info.title || '(no title)'} [${info.code || '????'}] · ` +
                (t ? t.name : 'no save found') + (info.flashId ? ` · flash ID ${hex(info.flashId)}` : '');
            this.el.saveInfo.classList.add('show');
            this.el.saveType.options[0].textContent = `Save type: auto (${t ? t.name : 'none'})`;
            this.log(`Game: ${info.title} [${info.code}], save: ${t ? t.name : 'none found'}`, 'success');
            if (info.type === 5 || info.type === 6) this.log('EEPROM size is worked out from the chip\'s contents; a completely blank 8 KB EEPROM shows as 512 B (choose 8 KB in the list if needed).', 'info');
        });
    }

    setSaveProgress(phase, done, total) {
        const pct = total ? Math.min(100, Math.floor(done * 100 / total)) : 0;
        this.el.saveProgressWrap.classList.add('show');
        this.el.saveProgressPhase.textContent = phase;
        this.el.saveProgressText.textContent = `${pct}%`;
        this.el.saveProgressFill.style.width = `${pct}%`;
    }

    async backupSave() {
        const type = this.chosenSaveType();
        if (!type) { this.log('Detect the save chip (or choose a save type) first.', 'error'); return; }
        await this.run(async () => {
            const link = this.newLink();
            await link.configure();
            this.log(`Backing up the save (${Burn2Slot.SAVE_TYPES[type].name})...`);
            this.setSaveProgress('Reading', 0, 1);
            const data = await Burn2Slot.readSave(link, type, (d, t) => this.setSaveProgress('Reading', d, t));
            const name = ((this.save && this.save.title) || 'save').replace(/[^\w\- ]+/g, '').trim().replace(/ +/g, '_') + '.sav';
            const url = URL.createObjectURL(new Blob([data], { type: 'application/octet-stream' }));
            const a = document.createElement('a');
            a.href = url; a.download = name; document.body.appendChild(a); a.click(); a.remove();
            setTimeout(() => URL.revokeObjectURL(url), 10000);
            this.setSaveProgress('Saved', 1, 1);
            this.log(`Save backed up: ${name} (${this.formatSize(data.length)})`, 'success');
        });
    }

    async restoreSave(file) {
        const data = new Uint8Array(await file.arrayBuffer());
        let type = Number(this.el.saveType.value);
        const sizeOf = t => Burn2Slot.SAVE_TYPES[t].size;
        if (!type) {
            type = this.save ? this.save.type : 0;
            // EEPROM: the file tells us which size; SRAM: a 64 KB file means the 64 KB layout
            if (type === 6 && data.length === 0x200) type = 5;
            if (type === 1 && data.length === 0x10000) type = 2;
        }
        if (!type) { this.log('Detect the save chip (or choose a save type) first.', 'error'); return; }
        let buf = data;
        if (data.length !== sizeOf(type)) {
            // emulator saves are sometimes padded (e.g. +16 bytes RTC data): use the save-sized start
            if (data.length > sizeOf(type) && data.length - sizeOf(type) <= 0x200) buf = data.slice(0, sizeOf(type));
            else { this.log(`${file.name} is ${this.formatSize(data.length)}, but this cart's save (${Burn2Slot.SAVE_TYPES[type].name}) is ${this.formatSize(sizeOf(type))}.`, 'error'); return; }
        }
        if (!confirm(`Replace the save on the cart with ${file.name}?`)) return;
        await this.run(async () => {
            const link = this.newLink();
            await link.configure();
            this.log(`Restoring ${file.name} (${Burn2Slot.SAVE_TYPES[type].name})...`);
            this.setSaveProgress('Writing', 0, 1);
            await Burn2Slot.writeSave(link, type, buf, (d, t) => this.setSaveProgress('Writing', d, t));
            this.setSaveProgress('Restored', 1, 1);
            this.log('Save restored and verified.', 'success');
        });
    }

    async connect() {
        try {
            if (this.usb && this.usb.isConnected) { try { await this.usb.disconnect(); } catch (_) {} }
            this.usb = this.transport === 'serial' ? new SerialConnection() : new UsbConnection();
            this.log(`Requesting ${this.transport === 'serial' ? 'serial port' : 'USB device'}...`);
            await this.usb.connect();
            this.el.usbStatus.textContent = `Connected (${this.transport})`;
            this.el.usbStatus.className = 'status connected';
            this.log('GB-Link connected.', 'success');
        } catch (e) {
            this.log(`Connection failed: ${e.message}`, 'error');
        }
        this.updateButtons();
    }

    async sendFlasher() {
        await this.run(async () => {
            const bin = Uint8Array.from(atob(window.BURN2SLOT_MB), c => c.charCodeAt(0));
            this.log('Sending the flasher. Now turn the GBA on while holding START + SELECT...');
            const ok = await window.GBAMultiboot.multiboot(this.usb, bin.buffer, (m, t) => this.log(m, t));
            if (!ok) { this.log('Multiboot failed. Turn the GBA off and try again.', 'error'); return; }
            await this.detectChip();
        });
    }

    async detect() { await this.run(() => this.detectChip()); }

    async detectChip() {
        this.link = new Burn2Slot.Burn2SlotLink(this.usb, (m, t) => this.log(m, t));
        await this.link.configure();
        this.log('Waiting for the flasher...');
        await this.link.command(Burn2Slot.CMD.HELLO, [], null, 5000);
        const r = await this.link.command(Burn2Slot.CMD.DETECT, [], null, 20000);
        const [flashid, maker, size] = r.data;
        const hex = (v, n) => '0x' + v.toString(16).toUpperCase().padStart(n, '0');
        if (r.status !== Burn2Slot.ST.OK) {
            this.el.chipInfo.innerHTML = `<span style="color: var(--error-color)">Unknown flash chip</span> (ID ${hex(flashid, 4)})`;
            this.el.chipInfo.classList.add('show');
            this.log(`Unknown flash chip (ID ${hex(flashid, 4)}) - this cart isn't supported by Burn2Slot.`, 'error');
            this.chip = null;
        } else {
            this.chip = { flashid, maker, size };
            this.el.chipInfo.textContent = `Flash ID ${hex(flashid, 4)} · ${this.makerName(maker)} · ` +
                (size ? `${size / 1048576} MB` : 'size unknown');
            this.el.chipInfo.classList.add('show');
            this.log('Flash chip detected. Choose a ROM and press "Flash to cart".', 'success');
        }
    }

    makerName(id) {
        return ({ 0x52: 'Alliance', 0x1F: 'Atmel', 0x04: 'Fujitsu', 0x89: 'Intel', 0xC2: 'Macronix', 0x62: 'Sanyo',
                  0xB0: 'Sharp', 0x01: 'Spansion', 0xBF: 'SST', 0x20: 'ST', 0xDA: 'Winbond', 0x1C: 'Mitsubishi' })[id & 0xFF] || 'Unknown maker';
    }

    async flash() {
        if (!this.romData) return;
        if (this.chip && this.chip.size && this.romData.length > this.chip.size) {
            this.log(`This ROM (${this.formatSize(this.romData.length)}) is bigger than the cart (${this.formatSize(this.chip.size)}).`, 'error');
            return;
        }
        await this.run(async () => {
            this.el.btnCancel.style.display = '';
            this.el.progressWrap.classList.add('show');
            this.setProgress('Erasing', 0, 1);
            this.link = new Burn2Slot.Burn2SlotLink(this.usb, (m, t) => this.log(m, t));
            try {
                await Burn2Slot.flashRom(this.usb, this.romData, {
                    link: this.link,
                    log: (m, t) => this.log(m, t),
                    onProgress: (phase, done, total) => this.setProgress(phase === 'erase' ? 'Erasing' : 'Writing', done, total),
                });
                this.setProgress('Done', 1, 1);
            } catch (e) {
                await Burn2Slot.reportFailure(this.usb, () => {});
                throw e;
            } finally {
                this.el.btnCancel.style.display = 'none';
            }
        });
    }

    async run(fn) {
        if (this.busy) return;
        this.busy = true;
        this.updateButtons();
        try { await fn(); }
        catch (e) { this.log(e.message === 'Cancelled' ? 'Cancelled.' : `Error: ${e.message}`, 'error'); }
        this.busy = false;
        this.updateButtons();
    }

    setProgress(phase, done, total) {
        const pct = total ? Math.min(100, Math.floor(done * 100 / total)) : 0;
        this.el.progressPhase.textContent = phase;
        this.el.progressText.textContent = `${pct}%`;
        this.el.progressFill.style.width = `${pct}%`;
    }

    updateButtons() {
        const connected = !!(this.usb && this.usb.isConnected);
        this.el.btnConnect.disabled = this.busy;
        this.el.btnMultiboot.disabled = !connected || this.busy;
        this.el.btnDetect.disabled = !connected || this.busy;
        this.el.btnFlash.disabled = !connected || this.busy || !this.romData;
        this.el.btnSendMb.disabled = !connected || this.busy || !this.mbData;
        this.el.btnSaveDetect.disabled = !connected || this.busy;
        this.el.saveType.disabled = !connected || this.busy;
        const haveType = !!this.chosenSaveType();
        this.el.btnSaveBackup.disabled = !connected || this.busy || !haveType;
        this.el.btnSaveRestore.disabled = !connected || this.busy || !haveType;
    }

    formatSize(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / 1048576).toFixed(1) + ' MB';
    }

    log(message, type = '') {
        const entry = document.createElement('div');
        entry.className = 'log-entry' + (type ? ' ' + type : '');
        entry.textContent = `[${new Date().toLocaleTimeString()}] ${message}`;
        this.el.log.appendChild(entry);
        this.el.log.scrollTop = this.el.log.scrollHeight;
    }
}

document.addEventListener('DOMContentLoaded', () => { window.app = new App(); });
