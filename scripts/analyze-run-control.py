#!/usr/bin/env python3
"""Decode timestamped DT5215 control requests from a classic Ethernet PCAP."""

from __future__ import annotations

import argparse
import collections
import dataclasses
import hashlib
import ipaddress
import json
import struct
from pathlib import Path


REQUEST_LENGTHS = {
    b"CINF": 6,
    b"ENUM": 6,
    b"CCNT": 12,
    b"CRRG": 8,
    b"CWRG": 12,
    b"RREG": 12,
    b"WREG": 16,
    b"FCMD": 20,
    b"DCMD": 20,
    b"SNT0": 4,
    b"RLNK": 4,
    b"RBIC": 4,
    b"CLRS": 4,
    b"VERS": 4,
}

COMMAND_NAMES = {
    0x11: "RESET_TIME",
    0x12: "ACQ_START",
    0x13: "ACQ_STOP",
    0x14: "SOFTWARE_TRIGGER",
    0x15: "GLOBAL_RESET",
    0x16: "TEST_PULSE",
    0x17: "RESET_PTRG",
    0x18: "CLEAR_DATA",
    0x1C: "SYNC",
    0x20: "CONFIGURE_ASIC",
}


@dataclasses.dataclass
class Segment:
    sequence: int
    timestamp: float
    payload: bytes


@dataclasses.dataclass
class Flow:
    client_ip: str
    client_port: int
    server_ip: str
    server_port: int
    syn: float | None = None
    fin: float | None = None
    client_segments: list[Segment] = dataclasses.field(default_factory=list)
    server_payload_first: float | None = None
    server_payload_last: float | None = None
    server_payload_bytes: int = 0


def packets(path: Path):
    with path.open("rb") as source:
        header = source.read(24)
        if len(header) != 24:
            raise ValueError("truncated PCAP header")
        magic = header[:4]
        if magic == b"\xd4\xc3\xb2\xa1":
            endian, scale = "<", 1_000_000
        elif magic == b"\x4d\x3c\xb2\xa1":
            endian, scale = "<", 1_000_000_000
        else:
            raise ValueError(f"unsupported PCAP magic {magic.hex()}")
        if struct.unpack_from(endian + "I", header, 20)[0] != 1:
            raise ValueError("only Ethernet PCAP captures are supported")
        record_number = 0
        while record := source.read(16):
            if len(record) != 16:
                raise ValueError(f"truncated record header {record_number}")
            seconds, fraction, captured, _ = struct.unpack(endian + "IIII", record)
            frame = source.read(captured)
            if len(frame) != captured:
                raise ValueError(f"truncated frame {record_number}")
            yield seconds + fraction / scale, frame
            record_number += 1


def tcp_packet(frame: bytes):
    if len(frame) < 14:
        return None
    ether_type = struct.unpack_from("!H", frame, 12)[0]
    offset = 14
    if ether_type == 0x8100 and len(frame) >= 18:
        ether_type = struct.unpack_from("!H", frame, 16)[0]
        offset = 18
    if ether_type != 0x0800 or len(frame) < offset + 20:
        return None
    ip = frame[offset:]
    ip_length = (ip[0] & 0x0F) * 4
    total_length = struct.unpack_from("!H", ip, 2)[0]
    if ip[9] != 6 or ip_length < 20 or len(ip) < total_length:
        return None
    tcp = ip[ip_length:total_length]
    if len(tcp) < 20:
        return None
    tcp_length = (tcp[12] >> 4) * 4
    if tcp_length < 20 or len(tcp) < tcp_length:
        return None
    return {
        "source_ip": str(ipaddress.ip_address(ip[12:16])),
        "destination_ip": str(ipaddress.ip_address(ip[16:20])),
        "source_port": struct.unpack_from("!H", tcp, 0)[0],
        "destination_port": struct.unpack_from("!H", tcp, 2)[0],
        "sequence": struct.unpack_from("!I", tcp, 4)[0],
        "flags": tcp[13],
        "payload": tcp[tcp_length:],
    }


def read_flows(path: Path, server_ip: str, ports: set[int]) -> list[Flow]:
    flows: dict[tuple[str, int, int], Flow] = {}
    for timestamp, frame in packets(path):
        packet = tcp_packet(frame)
        if packet is None:
            continue
        source_server = packet["source_ip"] == server_ip and packet["source_port"] in ports
        destination_server = (
            packet["destination_ip"] == server_ip
            and packet["destination_port"] in ports
        )
        if not source_server and not destination_server:
            continue
        if destination_server:
            client_ip, client_port = packet["source_ip"], packet["source_port"]
            server_port = packet["destination_port"]
        else:
            client_ip, client_port = packet["destination_ip"], packet["destination_port"]
            server_port = packet["source_port"]
        key = (client_ip, client_port, server_port)
        flow = flows.setdefault(
            key, Flow(client_ip, client_port, server_ip, server_port)
        )
        if destination_server:
            if packet["flags"] & 0x02 and not packet["flags"] & 0x10:
                flow.syn = timestamp
            if packet["payload"]:
                flow.client_segments.append(
                    Segment(packet["sequence"], timestamp, packet["payload"])
                )
        elif packet["payload"]:
            flow.server_payload_first = (
                timestamp
                if flow.server_payload_first is None
                else min(flow.server_payload_first, timestamp)
            )
            flow.server_payload_last = (
                timestamp
                if flow.server_payload_last is None
                else max(flow.server_payload_last, timestamp)
            )
            flow.server_payload_bytes += len(packet["payload"])
        if packet["flags"] & 0x05:
            flow.fin = timestamp if flow.fin is None else max(flow.fin, timestamp)
    # Ignore packets from connections whose SYN predates the capture. They are
    # unrelated stale sockets and cannot provide complete timing evidence.
    complete_flows = [flow for flow in flows.values() if flow.syn is not None]
    return sorted(
        complete_flows,
        key=lambda flow: (
            flow.syn if flow.syn is not None else float("inf"),
            flow.client_port,
        ),
    )


def reassemble(segments: list[Segment]) -> tuple[bytes, list[float]]:
    if not segments:
        return b"", []
    ordered = sorted(segments, key=lambda segment: (segment.sequence, segment.timestamp))
    stream = bytearray()
    timestamps: list[float] = []
    next_sequence = ordered[0].sequence
    for segment in ordered:
        start = segment.sequence
        end = start + len(segment.payload)
        if end <= next_sequence:
            continue
        if start > next_sequence:
            raise ValueError(f"TCP capture gap: {start} follows {next_sequence}")
        overlap = max(0, next_sequence - start)
        new_payload = segment.payload[overlap:]
        stream.extend(new_payload)
        timestamps.extend([segment.timestamp] * len(new_payload))
        next_sequence += len(new_payload)
    return bytes(stream), timestamps


def decode_requests(flow: Flow):
    stream, timestamps = reassemble(flow.client_segments)
    requests = []
    offset = 0
    while offset < len(stream):
        opcode = stream[offset : offset + 4]
        length = REQUEST_LENGTHS.get(opcode)
        if length is None:
            raise ValueError(
                f"{flow.client_ip}:{flow.client_port} unknown opcode "
                f"{opcode!r} at stream offset {offset}"
            )
        if offset + length > len(stream):
            raise ValueError(f"truncated {opcode.decode()} request at offset {offset}")
        request = stream[offset : offset + length]
        row = {"timestamp": timestamps[offset], "opcode": opcode.decode()}
        if opcode in {b"CINF", b"ENUM"}:
            row["chain"] = struct.unpack_from("<H", request, 4)[0]
        elif opcode == b"CCNT":
            row.update(
                chain=struct.unpack_from("<H", request, 4)[0],
                enabled=bool(struct.unpack_from("<H", request, 6)[0]),
                token_interval=struct.unpack_from("<I", request, 8)[0],
            )
        elif opcode in {b"RREG", b"WREG"}:
            row.update(
                chain=struct.unpack_from("<H", request, 4)[0],
                node=struct.unpack_from("<H", request, 6)[0],
                address=f"0x{struct.unpack_from('<I', request, 8)[0]:08x}",
            )
            if opcode == b"WREG":
                row["value"] = f"0x{struct.unpack_from('<I', request, 12)[0]:08x}"
        elif opcode in {b"CRRG", b"CWRG"}:
            row["address"] = f"0x{struct.unpack_from('<I', request, 4)[0]:08x}"
            if opcode == b"CWRG":
                row["value"] = f"0x{struct.unpack_from('<I', request, 8)[0]:08x}"
        elif opcode in {b"FCMD", b"DCMD"}:
            command = struct.unpack_from("<I", request, 8)[0]
            row.update(
                chain=struct.unpack_from("<H", request, 4)[0],
                node=struct.unpack_from("<H", request, 6)[0],
                command=f"0x{command:02x}",
                command_name=COMMAND_NAMES.get(command, "UNKNOWN"),
                delay=struct.unpack_from("<I", request, 16)[0],
            )
        requests.append(row)
        offset += length
    return stream, requests


def request_counts(requests):
    return dict(sorted(collections.Counter(row["opcode"] for row in requests).items()))


def command_counts(requests):
    return dict(
        sorted(
            collections.Counter(
                row["command_name"]
                for row in requests
                if row["opcode"] in {"FCMD", "DCMD"}
            ).items()
        )
    )


def summarize_session(flow: Flow, requests):
    def first_index(predicate, start=0):
        return next(
            index
            for index, request in enumerate(requests[start:], start)
            if predicate(request)
        )

    first_reset = first_index(
        lambda request: request.get("command_name") == "GLOBAL_RESET"
    )
    clear_stream = first_index(lambda request: request["opcode"] == "CLRS")
    start = first_index(lambda request: request.get("command_name") == "ACQ_START")
    stop = first_index(
        lambda request: request.get("command_name") == "ACQ_STOP", start + 1
    )

    def phase(name, begin, end, begin_time, end_time):
        selected = requests[begin:end]
        return {
            "name": name,
            "start_timestamp": begin_time,
            "end_timestamp": end_time,
            "duration_seconds": end_time - begin_time,
            "request_counts": request_counts(selected),
            "command_counts": command_counts(selected),
        }

    return {
        "boundary_definition": (
            "connection: TCP SYN to first per-board GLOBAL_RESET; "
            "configuration: first GLOBAL_RESET to CLRS; "
            "run setup: CLRS to ACQ_START; active acquisition: ACQ_START to ACQ_STOP"
        ),
        "phases": [
            phase(
                "connection",
                0,
                first_reset,
                flow.syn,
                requests[first_reset]["timestamp"],
            ),
            phase(
                "configuration",
                first_reset,
                clear_stream,
                requests[first_reset]["timestamp"],
                requests[clear_stream]["timestamp"],
            ),
            phase(
                "run_setup",
                clear_stream,
                start,
                requests[clear_stream]["timestamp"],
                requests[start]["timestamp"],
            ),
            phase(
                "active_acquisition",
                start,
                stop,
                requests[start]["timestamp"],
                requests[stop]["timestamp"],
            ),
            phase(
                "data_taking_through_stop",
                clear_stream,
                stop + 1,
                requests[clear_stream]["timestamp"],
                requests[stop]["timestamp"],
            ),
        ],
        "milestones_seconds_from_syn": {
            "first_global_reset": requests[first_reset]["timestamp"] - flow.syn,
            "clear_stream": requests[clear_stream]["timestamp"] - flow.syn,
            "acquisition_start": requests[start]["timestamp"] - flow.syn,
            "acquisition_stop": requests[stop]["timestamp"] - flow.syn,
        },
    }


def sha256_file(path: Path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("pcap", type=Path)
    parser.add_argument("--server-ip", default="172.16.0.11")
    parser.add_argument("--json", action="store_true")
    arguments = parser.parse_args()

    flows = read_flows(arguments.pcap, arguments.server_ip, {9760, 9000})
    capture_hash = sha256_file(arguments.pcap)
    result = {"pcap": str(arguments.pcap), "sha256": capture_hash, "flows": []}
    for flow in flows:
        row = dataclasses.asdict(flow)
        row.pop("client_segments")
        if flow.server_port == 9760:
            stream, requests = decode_requests(flow)
            row["client_payload_bytes"] = len(stream)
            row["request_counts"] = request_counts(requests)
            row["summary"] = summarize_session(flow, requests)
            row["requests"] = requests
        result["flows"].append(row)

    if arguments.json:
        print(json.dumps(result, indent=2))
        return
    print(f"PCAP {arguments.pcap} sha256={capture_hash}")
    for index, flow in enumerate(result["flows"], 1):
        print(
            f"flow {index}: {flow['client_ip']}:{flow['client_port']} -> "
            f"{flow['server_ip']}:{flow['server_port']} syn={flow['syn']} "
            f"fin={flow['fin']} server_payload_bytes={flow['server_payload_bytes']}"
        )
        if "request_counts" in flow:
            print(f"  requests: {flow['request_counts']}")


if __name__ == "__main__":
    main()
