#!/usr/bin/env python3
"""Open the containerized demo in a PTY and verify it loads ClickHouse data."""

import errno
import fcntl
import os
from pathlib import Path
import pty
import select
import struct
import subprocess
import termios
import time


def main():
    root = Path(__file__).resolve().parents[2]
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 50, 150, 0, 0))
    env = dict(os.environ, TERM="xterm-256color", NO_COLOR="1")
    process = subprocess.Popen(
        ["docker", "compose", "-f", "demo/compose.yaml", "--profile", "tui", "run", "--rm", "--no-deps", "tui"],
        cwd=root,
        env=env,
        stdin=slave,
        stdout=slave,
        stderr=slave,
        start_new_session=True,
    )
    os.close(slave)
    output = bytearray()
    deadline = time.monotonic() + 60
    expected = (b"FLOW OVERVIEW", b"DB: demo", b"audit_summary_mv")
    loaded = False
    try:
        while time.monotonic() < deadline:
            readable, _, _ = select.select([master], [], [], 0.2)
            if readable:
                try:
                    chunk = os.read(master, 65536)
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
                    chunk = b""
                output.extend(chunk)
            if not loaded and all(item in output for item in expected):
                os.write(master, b"q")
                loaded = True
                deadline = time.monotonic() + 15
            if process.poll() is not None:
                if not loaded:
                    raise RuntimeError(f"demo TUI exited before loading: {process.returncode}")
                if process.returncode != 0:
                    raise RuntimeError("demo TUI exited unsuccessfully")
                print("Demo TUI connected and displayed the overview.")
                return
        if loaded:
            raise TimeoutError("demo TUI did not exit within 15 seconds")
        raise TimeoutError("demo TUI did not load the overview within 60 seconds")
    except Exception:
        print(output[-16000:].decode("utf-8", "replace"))
        raise
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        os.close(master)


if __name__ == "__main__":
    main()
