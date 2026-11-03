"""Run deterministic save/transport/firmware tests without Android or test ROMs."""
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
src = root / "app/src/main/java/io/gblink/companion"
with tempfile.TemporaryDirectory(prefix="gblink-test-classes-") as directory:
    subprocess.run([
        "javac", "--release", "8", "-d", directory,
        *[str(src / (name + ".java")) for name in
          ("Wire", "Store", "CartridgeSync", "FirmwareImage")],
        str(root / "tests/CoreTests.java"),
    ], check=True)
    subprocess.run([
        "java", "-cp", directory, "io.gblink.companion.CoreTests",
        str(root / "app/src/main/assets/gblink.v2.2.7.uf2"),
    ], check=True)
