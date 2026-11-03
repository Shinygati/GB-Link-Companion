# Validation for the 0.1.0 alpha

Completed during this build:

- Android Java/resources compiled against SDK 35, min SDK 26.
- Native mGBA 0.10.5 plus the JNI frontend/save hook compiled for `arm64-v8a`, `armeabi-v7a` and `x86_64` with NDK r27c; 16 KB ELF load-segment alignment enabled.
- APK alignment and v2/v3 signing checks passed.
- **436 deterministic host checks passed** for fragmented USB frames, checksums, six save-chip sizes, pending journal restart, USB loss before/after a save block, wrong cartridges, changed saves, stale conflict confirmation, a newer emulator save during write-back, identical save-event backups, bundled UF2 hash/family/address validation, flash-sector preservation and failed read-back handling.
- **10 checks passed against the real mGBA JNI build on Windows**, using the same frontend code and save hook: load cartridge save memory at all six supported sizes, run an original tiny test ROM, render a green frame, read/modify SRAM, preserve the save tail, receive mGBA's `savedataUpdated` event, and unload/reload the core.

The Windows-native smoke test validates the emulator/frontend logic; it is not an Android runtime or USB hardware test. Android emulator startup was attempted but did not produce a usable boot in this environment. No real cartridge, GB-Link adapter, phone, firmware flash or Android UI session was available for end-to-end verification. The alpha remains hardware-unverified.

Before treating this as a daily-use cartridge writer, validate on the intended phone and a cartridge with an independently exported backup:

1. Start a new game session from a known cartridge save; compare the cartridge backup with the expected bytes.
2. Make an in-game save while connected. Check that the mGBA backup, write-back and full cartridge read-back agree.
3. Unplug GB-Link while playing, save again, exit, decline write-back, force-stop/reopen the app, and confirm the same pending save is offered.
4. Reconnect, start the GBA helper, write the pending save and load it on the physical GBA.
5. Repeat with each save chip family in use; test flash128K bank writes and both EEPROM sizes.
6. Change the cartridge save independently and confirm that the conflict prompt preserves both copies. Check a wrong game/revision is rejected.
7. Interrupt a save write using a disposable test save, reconnect and verify pending recovery/read-back.
8. Check GBA buttons, multitouch, gamepad controls, background/resume and Android Back/Home behavior.
9. Compare a dumped ROM to an independently known-good dump. Exercise cancellation and estimated-size reporting.
10. Check same/different/unknown firmware version prompts. Test the BOOTSEL USB permission transition, firmware-sector backup, read-back verification, reboot and reported-version recheck on the official RP2040 adapter. Test the exported UF2 with the official manual recovery procedure.
