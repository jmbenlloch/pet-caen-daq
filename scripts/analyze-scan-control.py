#!/usr/bin/env python3
"""Summarize DT5215 scan control requests exported by tshark.

Input is tab-separated frame.time_epoch, tcp.stream, tcp.payload for client
payloads sent to TCP port 9760.  This keeps PCAP decoding separate from the
protocol-specific interpretation and makes the study reproducible with either
host tshark or the repository's documented netshoot container.
"""

from __future__ import annotations

import argparse
import collections
import dataclasses
import struct
from pathlib import Path


TARGET_REGISTERS = {
    0x01000114: "QD_COARSE",
    0x01000118: "TD_COARSE",
    0x01000124: "HOLD_DELAY",
    0x01000350: "T_OR_CNT",
    0x01000354: "Q_OR_CNT",
}
TARGET_COMMANDS = {
    0x12: "ACQ_START",
    0x13: "ACQ_STOP",
    0x17: "RESET_PTRG",
    0x20: "CFG_ASIC",
}


@dataclasses.dataclass(frozen=True)
class Request:
    time: float
    stream: int
    opcode: str
    chain: int | None = None
    node: int | None = None
    argument: int | None = None
    value: int | None = None
    delay: int | None = None


def decode_line(line: str) -> Request | None:
    fields = line.rstrip("\n").split("\t")
    if len(fields) != 3:
        return None
    timestamp, stream, payload_hex = fields
    try:
        payload = bytes.fromhex(payload_hex)
    except ValueError:
        return None
    if len(payload) < 4:
        return None
    opcode = payload[:4].decode("ascii", "replace")
    if opcode in {"WREG", "FCMD", "DCMD"} and len(payload) >= 16:
        chain, node, argument, value = struct.unpack_from("<HHII", payload, 4)
        delay = struct.unpack_from("<I", payload, 16)[0] if len(payload) >= 20 else None
        return Request(float(timestamp), int(stream), opcode, chain, node, argument, value, delay)
    if opcode == "RREG" and len(payload) >= 12:
        chain, node, argument = struct.unpack_from("<HHI", payload, 4)
        return Request(float(timestamp), int(stream), opcode, chain, node, argument)
    return Request(float(timestamp), int(stream), opcode)


def label(request: Request) -> str | None:
    if request.opcode in {"WREG", "RREG"} and request.argument is not None:
        if request.argument in TARGET_REGISTERS:
            return TARGET_REGISTERS[request.argument]
        if request.argument & 0xFFC0FFFF == 0x02000800:
            return f"HIT_CNT_{(request.argument >> 16) & 0x3F}"
    if request.opcode in {"FCMD", "DCMD"} and request.argument in TARGET_COMMANDS:
        return TARGET_COMMANDS[request.argument]
    return None


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("control_tsv", type=Path)
    args = parser.parse_args()

    requests = [
        request
        for line in args.control_tsv.read_text().splitlines()
        if (request := decode_line(line)) is not None
    ]
    counts = collections.Counter(request.opcode for request in requests)
    print(f"file={args.control_tsv}")
    print("opcode_counts=" + " ".join(f"{key}:{counts[key]}" for key in sorted(counts)))

    selected = [request for request in requests if label(request) is not None]
    if selected:
        print(f"selected_span_s={selected[-1].time - selected[0].time:.6f}")
    for request in selected:
        detail = ""
        if request.opcode == "WREG":
            detail = f" value={request.value}"
        elif request.opcode in {"FCMD", "DCMD"}:
            detail = f" delay={request.delay}"
        print(
            f"{request.time:.6f} {request.opcode} "
            f"{request.chain}:{request.node} {label(request)}{detail}"
        )


if __name__ == "__main__":
    main()
