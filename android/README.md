# GB-Link Companion for Android — 0.1.0 alpha

Android port of [Shinygati/GB-Link-Companion](https://github.com/Shinygati/GB-Link-Companion), based on commit `23638b2a741672db86f02402c54b76e7c57f61a3`. It embeds **mGBA 0.10.5**, rather than launching a separate Android emulator. Original icons and the original GBA helper are reused with attribution.

## Play

Requires Android 8.0 or newer with USB host/OTG, a GB-Link **RP2040** adapter, a **Game Boy Color link cable**, and a powered **GBA or GBA SP** containing a supported GBA cartridge. The adapter is a link cable interface; it is not a standalone cartridge reader. ESP32 Switch bridge hardware is not used.

1. Add an uncompressed `.gba` ROM from phone storage. The app imports a copy into its private library. No commercial ROMs or Nintendo BIOS are included.
2. Connect GB-Link to the phone with USB OTG. Insert the matching cartridge. Switch on the GBA while holding START + SELECT. Tap **Connect GB-Link** and grant Android USB access.
3. The app checks the adapter firmware and sends the upstream GBA helper by multiboot. Tap **Play with cartridge save**. The ROM header/revision must match the cartridge. The app reads and backs up the cartridge save before loading it into mGBA.
4. Play using phone touch controls, an Android gamepad, or the connected GBA's buttons. **Use the game's normal Save command.** Emulator save states are not used for cartridge write-back.
5. Connected saves are journaled, backed up, copied to the cartridge and read back in full. You may unplug GB-Link and keep playing. Pending phone saves survive ordinary app/process restarts.
6. **Home / Back** pauses and checkpoints the game, then offers write-back or keeping the save pending. The main screen also prompts. Reconnect GB-Link, restart the GBA helper if needed, and choose **Write to cartridge**. Alternatively resume the pending phone session or export its `.sav`.

Each *new* session loads its save from the cartridge. A pending session is never overwritten by starting a new cartridge read. Resuming a pending phone session is an explicit recovery option.

## Backups and conflicts

- Timestamped raw `.sav` backups are made for cartridge loads, pre-write reads, verification reads, each mGBA `savedataUpdated` event (including identical rewrites), and changed lifecycle checkpoints. mGBA's save-complete callback is used, with a stable-memory fallback.
- In-game saves remain available without the adapter. Pending data and expected cartridge save hashes use a persistent journal and immutable snapshots.
- A different header/revision or save chip is rejected. If the cartridge save changed while disconnected, the app keeps both and asks before replacement. A confirmation is bound to the exact cartridge save hash; it cannot authorize a later changed save.
- Interrupted block writes keep the pending phone copy. Known completed intermediate states can be retried. Unknown/partial chip states require explicit conflict replacement after a backup.
- Cartridge headers do not provide a unique physical cartridge serial number. Two copies of the same game are distinguished by their save contents, not by a unique hardware identifier.
- **Cartridge tools → Export an earlier backup** exports timestamped snapshots. Pending save cards also offer export. Backups are not automatically pruned. Export important saves before uninstalling or clearing app storage; Android private app data is removed by those actions. A force-stop or power loss in the middle of an uncheckpointed game write can lose the last in-memory changes.

## Cartridge tools

- Back up / export the cartridge save.
- Import an exact-size raw `.sav`, back up the cartridge copy, and write / verify it. Import the matching ROM first so recovery remains tied to its journal.
- Dump a GBA cartridge ROM, checking each transfer's checksum; add it to the phone library and optionally export it. An estimated helper ROM size is reported. Transfer checksums do **not** certify a known-good dump.
- Supported save chip formats: SRAM/FRAM 32 KB, SRAM 64 KB, flash 64 KB / 128 KB, EEPROM 512 B / 8 KB.

## Bundled adapter firmware

The app bundles the unmodified official **GB-Link RP2040 firmware 2.2.3**, the upstream stable release selected for this build:

- [Release and source](https://github.com/GB-Link/GBLink-Firmware/releases/tag/v2.2.3)
- UF2 SHA-256: `6600b227795434d202b186835c45bf4e3dfa505711c773a195592ccdf5df2113`
- Source commit: `4aef46a1aff688cc0a68df666858eccb9b5ae291`

Connection queries firmware command `0x0F`. A different or unknown version prompts **Install** or **Use installed version**. Installing the bundled version may downgrade a newer installed version; the prompt says so. The app does not silently update firmware.

Use **Settings → Install bundled GB-Link firmware**. It can send the adapter's `0x43` BOOTSEL reboot command. On old/nonresponsive firmware, reconnect the adapter while holding BOOTSEL. Confirm that the detected bootloader is the GB-Link adapter, grant USB access, and install. Android USB host PICOBOOT communicates directly with RP2040 boot ROM (`2E8A:0003`); no PC or mass-storage mount is required.

Before erasing, the app verifies the bundled SHA-256, validates every UF2 block and RP2040 family ID, and stores all affected original flash sectors in `files/firmware-backups/*.gbfw`. Uncovered bytes and flash sectors are preserved. Every changed sector is read back before reboot. Reconnect and tap Connect to confirm the firmware-reported version. The bundled UF2 can also be exported for the official manual BOOTSEL recovery procedure. Firmware recovery archives use a Java DataOutputStream header, record count, then big-endian address + 4096 bytes per affected sector; they are not raw UF2 files.

## Scope and validation

Implemented: GBA ROM library and integrated mGBA, cartridge-backed saves, detachable play and pending-save prompts, backups, save restoration, ROM dumping, GBA button input, phone/gamepad controls, and bundled RP2040 firmware version check / USB installer.

Pokémon status/second screens are omitted. The desktop app's GB/GBC cartridge mode, Pokémon Banks, Wonder Cards, flash-cart ROM programming, screenshots and desktop integrations are **not ported** in this alpha. This is not complete desktop feature parity.

The Android Java code compiles; native mGBA builds for ARM64, ARMv7 and x86-64; the APK is aligned and its v2/v3 signatures are checked. Host tests cover framing, all six save sizes, conflicts, interrupted writes, journal restart, concurrent newer saves, backup creation, UF2 validation, sector preservation and firmware verification failure. These are simulated transport/flash tests. **Real Android phone + GB-Link hardware testing is still required.** Do not infer physical hardware verification from a passing build or host test. See `VALIDATION.md`.

## Build

Open `android/` in Android Studio, using JDK 17, Android SDK 35, NDK `27.2.12479018`, CMake `3.22.1`, and the included Gradle 8.11.1 wrapper. The source ZIP includes the third-party source trees. For a Git checkout:

```sh
git submodule update --init --recursive
cd android
./gradlew assembleDebug
```

PowerShell: `./gradlew.bat assembleDebug`. The output is `app/build/outputs/apk/debug/app-debug.apk`. The supplied alpha APK is signed with a development key; it is not a Play Store release.

Run portable host integrity tests with JDK 17+ and Python 3:

```sh
python tests/run_tests.py
```

`tests/NativeSmoke.java` additionally exercises the real JNI/mGBA implementation using the original minimal test program `native-smoke.s`. Compile the native CMake project for the host (requires JNI headers), then run `python tests/run_native_smoke.py --library-dir PATH_TO_NATIVE_LIBRARY`. Use `--prefix PATH_TO/arm-none-eabi-` for devkitARM / arm-none-eabi binutils that are not on PATH. The runner generates the test header. No commercial game data is used.

## Source and licenses

- Original project, GBA helper and icon assets: Shinygati and upstream contributors, GNU GPL v3. Original helper source/build scripts remain in the repository's `b2s/` tree. The helper still contains its original drawing code; the Android host never sends Pokémon status-screen commands or advertises that menu.
- Android port: GNU GPL v3; see repository `LICENSE`.
- mGBA: Jeffrey Pfau and contributors, Mozilla Public License 2.0, pinned at `26b7884bc25a5933960f3cdcd98bac1ae14d42e2` (`0.10.5`). Its source is unmodified; CMake generates a frontend translation unit that includes the Android save hook from `app/src/main/cpp/companion_hooks.h`.
- GB-Link firmware: GB-Link / Celio-Link contributors; GPL v3 and component notices in `third_party/gblink-firmware`. Its build workflow and west manifest describe its Zephyr dependencies.
- PICOBOOT protocol port: based on Raspberry Pi picotool; copyright (c) 2020 Raspberry Pi (Trading) Ltd., BSD-3-Clause. Full notice is in APK assets.

Not affiliated with GB-Link or mGBA. License files are bundled in APK assets and the source distribution.
