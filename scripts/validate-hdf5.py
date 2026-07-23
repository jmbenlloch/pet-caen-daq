#!/usr/bin/env python3
"""Independent pet-caen-daq HDF5 schema and reference validator."""

import argparse
import hashlib
import json
import sys

import h5py
import numpy as np


KINDS = {
    1: "spectroscopy",
    2: "timing",
    3: "counting",
    4: "waveform",
    5: "service",
    6: "test",
}

REQUIRED_FIELDS = {
    "configuration/effective/boards": (
        "board", "chain", "node", "product_id", "firmware_revision",
        "acquisition_state",
    ),
    "configuration/effective/channels": (
        "board", "channel", "chip", "chip_channel", "readout_enabled",
        "qd_enabled", "td_enabled", "qd_fine", "td_fine", "high_gain",
        "low_gain", "hv_adjustment", "calibrate_high_gain",
        "calibrate_low_gain", "preamplifier_disabled",
    ),
    "configuration/effective/citiroc_chips": (
        "board", "chip", "discriminator_mask", "charge_coarse_threshold",
        "time_coarse_threshold", "low_shaping_time_code",
        "high_shaping_time_code", "fast_shaper_on_low_gain",
        "enable_input_dac", "input_dac_reference_45v",
        "enable_digital_output", "enable_or32", "enable_open_collector_or32",
        "negative_trigger_polarity", "enable_open_collector_time_or",
        "enable_channel_triggers",
    ),
    "configuration/effective/citiroc_stream_words": (
        "board", "chip", "word_index", "bit_count", "word",
    ),
    "configuration/effective/fpga_writes": (
        "board", "ordinal", "address", "value",
    ),
    "configuration/effective/hv_plans": (
        "board", "voltage_v", "current_limit_ma", "temperature_feedback",
        "feedback_mv_per_c", "coefficient_0", "coefficient_1",
        "coefficient_2",
    ),
    "configuration/effective/hv_transactions": (
        "board", "ordinal", "register", "data_type", "data",
    ),
    "configuration/effective/pedestal_plans": (
        "board", "common", "acquisition_mode", "zero_suppress_low_gain",
        "zero_suppress_high_gain", "per_channel", "calibration_present",
    ),
    "configuration/effective/pedestal_channels": (
        "board", "channel", "zero_suppress_low_gain",
        "zero_suppress_high_gain", "calibration_present",
        "low_gain_pedestal", "high_gain_pedestal",
    ),
    "events/index": (
        "sequence", "kind", "chain", "node", "qualifier", "kind_row",
        "trigger_id", "timestamp", "payload_offset_words",
        "payload_size_words", "crc_error",
    ),
    "events/spectroscopy/events": (
        "trigger_id", "timestamp", "validity", "relative_timestamp_clock",
        "channel_mask", "energy_offset", "energy_count", "timing_offset",
        "timing_count", "time_reference",
    ),
    "events/spectroscopy/energies": (
        "parent_row", "channel", "low_gain", "high_gain", "has_low_gain",
        "has_high_gain", "discriminator",
    ),
    "events/spectroscopy/timings": ("parent_row", "channel", "toa", "tot"),
    "events/timing/events": (
        "trigger_id", "timestamp", "time_reference", "hit_offset", "hit_count",
    ),
    "events/timing/hits": ("parent_row", "channel", "toa", "tot"),
    "events/counting/events": (
        "trigger_id", "timestamp", "validity", "relative_timestamp_clock",
        "channel_mask", "count_offset", "count_count", "t_or_count",
        "q_or_count",
    ),
    "events/counting/counts": ("parent_row", "channel", "counter_value"),
    "events/waveform/events": (
        "trigger_id", "timestamp", "sample_offset", "sample_count",
    ),
    "events/waveform/samples": (
        "parent_row", "sample_index", "high_gain", "low_gain", "digital_probes",
    ),
    "events/service/events": (
        "timestamp", "version", "format", "validity", "fpga_temperature_c",
        "board_temperature_c", "detector_temperature_c", "hv_temperature_c",
        "hv_voltage_v", "hv_current_a", "hv_on", "hv_ramping",
        "hv_over_current", "hv_over_voltage", "status", "counter_offset",
        "counter_count", "t_or_count", "q_or_count", "unknown_offset",
        "unknown_count",
    ),
    "events/service/counters": ("parent_row", "channel", "counter_value"),
    "events/test/events": ("trigger_id", "timestamp", "word_offset", "word_count"),
}


def fail(message):
    raise ValueError(message)


def is_little_endian(dtype):
    return dtype.byteorder in ("<", "|") or (
        dtype.byteorder == "=" and sys.byteorder == "little"
    )


def require_dataset(handle, name):
    if name not in handle:
        fail(f"missing dataset /{name}")
    dataset = handle[name]
    if not isinstance(dataset, h5py.Dataset):
        fail(f"/{name} is not a dataset")
    if len(dataset.shape) != 1:
        fail(f"/{name} has rank {len(dataset.shape)}, want 1")
    return dataset


def validate_compounds(handle):
    for name, expected in REQUIRED_FIELDS.items():
        dataset = require_dataset(handle, name)
        names = dataset.dtype.names
        if names != expected:
            fail(f"/{name} fields {names!r}, want {expected!r}")
        for field in names:
            dtype = dataset.dtype.fields[field][0]
            if dtype.kind in "uif" and not is_little_endian(dtype):
                fail(f"/{name}.{field} is not fixed-width little-endian: {dtype}")
    for name, kind, size in (
        ("events/service/unknown_payload", "u", 1),
        ("events/test/words", "u", 4),
        ("configuration/requested_janus", "u", 1),
        ("configuration/audit_json", "u", 1),
        ("configuration/effective_json", "u", 1),
        ("run/metadata_json", "u", 1),
        ("run/manifest_json", "u", 1),
    ):
        dtype = require_dataset(handle, name).dtype
        if dtype.kind != kind or dtype.itemsize != size or not is_little_endian(dtype):
            fail(f"/{name} has unexpected dtype {dtype}")


def decode_json_dataset(handle, name):
    try:
        return json.loads(bytes(require_dataset(handle, name)[:]))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        fail(f"/{name} is not valid UTF-8 JSON: {error}")


def validate_metadata(handle):
    metadata = decode_json_dataset(handle, "run/metadata_json")
    manifest = decode_json_dataset(handle, "run/manifest_json")
    if manifest.get("run_id") != bytes(handle["run/run_id"][:]).decode("utf-8"):
        fail("/run/manifest_json run_id does not match /run/run_id")
    # Creation metadata is produced by the HDF5 adapter and must contain the
    # identity block. Historical JSON manifests predate this additive schema-v1
    # metadata, so validate their identity only when it is present.
    for name, document in (("metadata_json", metadata),):
        configuration = document.get("configuration_identity")
        if not isinstance(configuration, dict):
            fail(f"/run/{name} is missing configuration_identity")
        execution = document.get("execution_identity")
        if not isinstance(execution, dict):
            fail(f"/run/{name} is missing execution_identity")
        for field in ("topology", "software", "storage", "runtime"):
            if field not in execution:
                fail(f"/run/{name} execution_identity is missing {field}")
        for field, dataset in (
            ("requested_configuration_sha256", "configuration/requested_janus"),
            ("effective_configuration_sha256", "configuration/effective_json"),
            ("configuration_audit_sha256", "configuration/audit_json"),
        ):
            recorded = configuration.get(field, "")
            if recorded and recorded != hashlib.sha256(bytes(handle[dataset][:])).hexdigest():
                fail(f"/run/{name} {field} does not match /{dataset}")
    if "execution_identity" in manifest:
        execution = manifest["execution_identity"]
        if not isinstance(execution, dict):
            fail("/run/manifest_json has invalid execution_identity")


def validate_configuration_tables(handle):
    effective = decode_json_dataset(handle, "configuration/effective_json")
    plans = effective if isinstance(effective, list) else []
    metadata = decode_json_dataset(handle, "run/metadata_json")
    boards = (
        metadata.get("execution_identity", {})
        .get("topology", {})
        .get("boards", [])
    )
    expected = {
        "boards": len(boards),
        "channels": len(plans) * 64,
        "citiroc_chips": len(plans) * 2,
        "citiroc_stream_words": len(plans) * 2 * 36,
        "fpga_writes": sum(len(plan.get("Writes", [])) for plan in plans),
        "hv_plans": len(plans),
        "hv_transactions": sum(
            len(plan.get("HV", {}).get("Transactions", [])) for plan in plans
        ),
        "pedestal_plans": len(plans),
        "pedestal_channels": len(plans) * 64,
    }
    for name, count in expected.items():
        actual = len(handle[f"configuration/effective/{name}"])
        if actual != count:
            fail(
                f"/configuration/effective/{name} has {actual} rows, "
                f"effective plan requires {count}"
            )
    channels = handle["configuration/effective/channels"][:]
    if len(channels):
        if np.any(channels["channel"] > 63):
            fail("/configuration/effective/channels has channel outside [0,63]")
        if np.any(channels["chip"] != channels["channel"] // 32):
            fail("/configuration/effective/channels has incorrect chip mapping")
        if np.any(channels["chip_channel"] != channels["channel"] % 32):
            fail("/configuration/effective/channels has incorrect chip-channel mapping")
    streams = handle["configuration/effective/citiroc_stream_words"][:]
    if len(streams):
        if np.any(streams["word_index"] > 35) or np.any(streams["bit_count"] != 1144):
            fail("/configuration/effective/citiroc_stream_words has invalid layout")


def validate_compression(handle):
    name = bytes(handle["run/compression"][:]).decode("ascii")
    if name == "none":
        return
    if name != "blosc-lz4-level4-bitshuffle":
        fail(f"unsupported recorded compression {name!r}")
    # h5py's high-level compression property is None for third-party filters.
    # Inspect the HDF5 creation property list directly.
    checked = 0
    def check(_, item):
        nonlocal checked
        if not isinstance(item, h5py.Dataset) or item.chunks is None:
            return
        filters = [
            item.id.get_create_plist().get_filter(index)
            for index in range(item.id.get_create_plist().get_nfilters())
        ]
        matches = [entry for entry in filters if entry[0] == 32001]
        if len(matches) != 1:
            fail(f"{item.name} does not have exactly one Blosc filter")
        values = matches[0][2]
        if len(values) < 7 or values[4:7] != (4, 2, 1):
            fail(f"{item.name} Blosc parameters {values}, want level=4 bitshuffle lz4")
        checked += 1
    handle["events"].visititems(check)
    if checked == 0:
        fail("compressed file has no filtered event datasets")


def validate_range(name, parents, offset_name, count_name, children):
    offsets = parents[offset_name].astype(np.uint64)
    counts = parents[count_name].astype(np.uint64)
    ends = offsets + counts
    if np.any(ends < offsets) or np.any(ends > len(children)):
        fail(f"/{name} contains an out-of-range child reference")
    if "parent_row" in (children.dtype.names or ()):
        actual = children["parent_row"][:]
        expected = np.full(len(children), np.iinfo(np.uint64).max, dtype=np.uint64)
        for parent, (offset, end) in enumerate(zip(offsets, ends)):
            expected[offset:end] = parent
        if not np.array_equal(actual, expected):
            fail(f"/{name} has an orphaned or incorrectly parented child row")


def validate_references(handle):
    index = handle["events/index"][:]
    expected_sequence = np.arange(1, len(index) + 1, dtype=np.uint64)
    if not np.array_equal(index["sequence"], expected_sequence):
        fail("/events/index sequence is not contiguous from 1")
    for kind, group in KINDS.items():
        rows = handle[f"events/{group}/events"]
        selected = index[index["kind"] == kind]["kind_row"]
        if len(selected) != len(rows) or not np.array_equal(
            np.sort(selected), np.arange(len(rows), dtype=np.uint64)
        ):
            fail(f"/events/index does not commit every {group} row exactly once")
    unknown = set(np.unique(index["kind"]).tolist()) - set(KINDS)
    if unknown:
        fail(f"/events/index contains unknown kinds {sorted(unknown)}")

    validate_range(
        "events/spectroscopy/energies",
        handle["events/spectroscopy/events"][:], "energy_offset", "energy_count",
        handle["events/spectroscopy/energies"],
    )
    validate_range(
        "events/spectroscopy/timings",
        handle["events/spectroscopy/events"][:], "timing_offset", "timing_count",
        handle["events/spectroscopy/timings"],
    )
    validate_range(
        "events/timing/hits", handle["events/timing/events"][:],
        "hit_offset", "hit_count", handle["events/timing/hits"],
    )
    validate_range(
        "events/counting/counts", handle["events/counting/events"][:],
        "count_offset", "count_count", handle["events/counting/counts"],
    )
    validate_range(
        "events/waveform/samples", handle["events/waveform/events"][:],
        "sample_offset", "sample_count", handle["events/waveform/samples"],
    )
    validate_range(
        "events/service/counters", handle["events/service/events"][:],
        "counter_offset", "counter_count", handle["events/service/counters"],
    )
    validate_range(
        "events/service/unknown_payload", handle["events/service/events"][:],
        "unknown_offset", "unknown_count", handle["events/service/unknown_payload"],
    )
    validate_range(
        "events/test/words", handle["events/test/events"][:],
        "word_offset", "word_count", handle["events/test/words"],
    )


def validate(path, require_complete):
    with h5py.File(path, "r") as handle:
        if int(handle.attrs.get("schema_version", -1)) != 1:
            fail("unsupported or missing schema_version attribute")
        complete = int(handle.attrs.get("complete", -1))
        if complete not in (0, 1):
            fail("invalid or missing complete attribute")
        if require_complete and complete != 1:
            fail("HDF5 file is incomplete")
        validate_compounds(handle)
        validate_metadata(handle)
        validate_configuration_tables(handle)
        validate_compression(handle)
        validate_references(handle)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("path")
    parser.add_argument("--allow-incomplete", action="store_true")
    arguments = parser.parse_args()
    try:
        validate(arguments.path, not arguments.allow_incomplete)
    except (OSError, ValueError) as error:
        print(f"HDF5 validation failed: {error}", file=sys.stderr)
        return 1
    print(f"HDF5 validation passed: {arguments.path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
