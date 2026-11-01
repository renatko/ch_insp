#!/usr/bin/env python3
"""Record the local synthetic demo and render an animated GIF with agg."""

import argparse
import codecs
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time

try:
    import pyte
except ModuleNotFoundError:
    sys.exit("Install the recording dependency: python3 -m pip install pyte==0.8.2")


ROOT = Path(__file__).resolve().parents[1]
COLS, ROWS = 120, 34


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=int(os.environ.get("DEMO_STANDALONE_PORT", "19100")))
    parser.add_argument("--output", type=Path, default=ROOT / "docs/assets/demo.gif")
    parser.add_argument("--agg", default="agg", help="path to the asciinema agg executable")
    parser.add_argument("--font-family", default="JetBrains Mono", help="monospace font available to agg (default: JetBrains Mono)")
    args = parser.parse_args()
    agg = shutil.which(args.agg)
    if not agg:
        sys.exit("agg not found; install https://github.com/asciinema/agg or pass --agg PATH")

    with tempfile.TemporaryDirectory(prefix="chinspector-recording-") as temporary:
        binary = Path(temporary) / "ch_insp"
        cast = Path(temporary) / "demo.cast"
        gif = Path(temporary) / "demo.gif"
        subprocess.run(["go", "build", "-o", str(binary), "."], cwd=ROOT, check=True)
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor")
        env.pop("NO_COLOR", None)
        try:
            process = subprocess.Popen(
                [str(binary), "--host", "localhost", "--port", str(args.port), "--user", "default", "--database", "demo"],
                cwd=ROOT, env=env, stdin=slave, stdout=slave, stderr=slave, start_new_session=True,
            )
        finally:
            os.close(slave)
        screen = pyte.Screen(COLS, ROWS)
        stream = pyte.Stream(screen)
        decoder = codecs.getincrementaldecoder("utf-8")("replace")
        prelude = []
        recording = False
        started = 0.0
        output = None

        def pump(seconds):
            until = time.monotonic() + seconds
            while time.monotonic() < until:
                ready, _, _ = select.select([master], [], [], min(0.1, max(0, until - time.monotonic())))
                if not ready:
                    continue
                try:
                    data = os.read(master, 65536)
                except OSError:
                    break
                if not data:
                    break
                text = decoder.decode(data)
                stream.feed(text)
                if recording:
                    output.write(json.dumps([round(time.monotonic() - started, 3), "o", text], ensure_ascii=False) + "\n")
                else:
                    prelude.append(text)

        def expect(text):
            for _ in range(75):
                if any(text in line for line in screen.display):
                    return
                if process.poll() is not None:
                    break
                pump(0.2)
            raise RuntimeError(f"Expected {text!r} in demo TUI:\n" + "\n".join(screen.display))

        def key(value, delay=0.35):
            os.write(master, value)
            pump(delay)

        def type_text(value):
            for char in value:
                key(char.encode(), 0.045)

        try:
            expect("FLOW OVERVIEW")
            expect("audit_summary_mv")
            output = cast.open("w", encoding="utf-8")
            output.write(json.dumps({"version": 2, "width": COLS, "height": ROWS, "timestamp": 0, "env": {"TERM": "xterm-256color"}}) + "\n")
            output.write(json.dumps([0, "o", "".join(prelude)], ensure_ascii=False) + "\n")
            started, recording = time.monotonic(), True
            pump(1.5)

            key(b"/")
            type_text("page_views_mv")
            expect("SEARCH FLOWS")
            pump(1.2)
            key(b"\r", 1)
            expect("page_views_mv")
            pump(1.2)

            key(b"f")
            expect("FILTER MATERIALIZED VIEWS")
            pump(0.7)
            key(b"/")
            type_text("page_views")
            key(b"\r")
            key(b" ")
            expect("Selected: demo.page_views")
            pump(0.8)
            key(b"\r", 1)
            expect("Source: demo.page_views")
            pump(1.2)

            key(b"/")
            type_text("page_views_mv")
            key(b"\r", 1)
            key(b"\r", 1)
            expect("DATA FLOW")
            pump(1.4)
            key(b"t", 1)
            expect("TARGET TABLE")
            expect("event_time")
            pump(1.7)
            key(b"\x1b[B")
            pump(0.7)
            key(b"\x1b", 0.7)
            key(b"c", 1)
            expect("COLUMN FLOW")
            expect("QUERY CONDITIONS")
            pump(2)
            key(b"d", 0.8)
            expect("CREATE DEFINITION")
            pump(2.3)
            key(b"\x1b", 0.6)
            key(b"\x1b", 0.8)
            expect("FLOW OVERVIEW")
            pump(1)
            recording = False
            output.close()
            output = None
            os.write(master, b"q")
            pump(0.7)
            if process.wait(timeout=5) != 0:
                raise RuntimeError("demo TUI exited unsuccessfully")
        finally:
            if output is not None:
                output.close()
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            os.close(master)

        command = [agg, "--quiet", "--font-size", "15", "--line-height", "1.25", "--theme", "github-dark", "--fps-cap", "8", "--idle-time-limit", "2", "--last-frame-duration", "1"]
        command.extend(["--font-family", args.font_family])
        subprocess.run(command + [str(cast), str(gif)], check=True)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(gif, args.output)
        print(f"Recorded demo GIF: {args.output}")


if __name__ == "__main__":
    main()
