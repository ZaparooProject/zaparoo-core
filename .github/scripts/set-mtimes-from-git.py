"""Give every tracked file and directory a modification time taken from its
git object hash.

A fresh checkout stamps every file with the time of the checkout. Go's test
cache records the size and modification time of each file a test opens, so
with checkout times no test result restored from an earlier run ever matches.
A time derived from the content hash is the same in every checkout of the
same content and changes whenever the content does, which lets packages whose
inputs did not change report (cached).
"""

import os
import subprocess

# Times fall in the twenty years from 2000, so every one is in the past and
# old enough for Go to cache a result that read it.
BASE = 946684800
SPAN = 20 * 365 * 86400


def stamp(path: bytes, obj: bytes) -> None:
    t = BASE + int(obj[:8], 16) % SPAN
    try:
        if os.path.islink(path):
            if os.utime in os.supports_follow_symlinks:
                os.utime(path, (t, t), follow_symlinks=False)
            return
        os.utime(path, (t, t))
    except FileNotFoundError:
        pass


def main() -> None:
    root = subprocess.run(
        ["git", "rev-parse", "HEAD^{tree}"], capture_output=True, check=True
    ).stdout.strip()
    stamp(b".", root)

    listing = subprocess.run(
        ["git", "ls-tree", "-r", "-t", "-z", "HEAD"], capture_output=True, check=True
    ).stdout
    for entry in listing.split(b"\0"):
        if not entry:
            continue
        meta, path = entry.split(b"\t", 1)
        _, kind, obj = meta.split(b" ")
        if kind in (b"blob", b"tree"):
            stamp(path, obj)


if __name__ == "__main__":
    main()
