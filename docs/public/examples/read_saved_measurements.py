#!/usr/bin/env python3
"""Read-only summary of a normal run or scan directory.

Install: python -m pip install numpy h5py hdf5plugin
Usage: python read_saved_measurements.py path/to/run-34
       python read_saved_measurements.py path/to/scan-100
"""
import argparse
import json
import re
from pathlib import Path

import h5py
import hdf5plugin  # Registers the compression filters used in the saved files.

FIELDS = {
    "fpga_temperature_c": 0,
    "board_temperature_c": 1,
    "detector_temperature_c": 2,
    "hv_temperature_c": 3,
    "hv_voltage_v": 4,
    "hv_current_a": 5,
}
PARAMETERS = {
    "HG_ShapingTime", "LG_ShapingTime", "HoldDelay", "HV_Vbias",
    "HV_Adjust_Range", "HV_IndivAdj", "TD_CoarseThreshold",
    "FastShaperInput", "PresetCounts", "TestPulseSource", "TestPulseDestination",
}


def text(dataset):
    return dataset[:].tobytes().decode("utf-8")


def summarize(folder):
    manifest = json.loads((folder / "manifest.json").read_text(encoding="utf-8"))
    normal = "run_id" in manifest
    records, observations = [], []
    spectroscopy_events = 0
    per_channel_names = set()
    if normal:
        audit = manifest.get("configuration_audit", {})
        per_channel_names = set(re.findall(
            r"^\s*(\w+)\[\d+\]\[\d+\]", manifest.get("requested_configuration", ""), re.MULTILINE))
        # Numbered segments only: exclude the companion histogram file.
        segments = sorted(folder.glob(f"run_{manifest['run_id']}.[0-9][0-9][0-9][0-9].h5"))
        if not segments:
            raise ValueError("No numbered HDF5 event segments found")
        for segment in segments:
            with h5py.File(segment, "r") as f:
                services = f["events/service/events"][:]
                index = f["events/index"][:]
                mapping = {(int(b["chain"]), int(b["node"])): int(b["board"])
                           for b in f["configuration/effective/boards"][:]}
                spectroscopy_events += len(f["events/spectroscopy/events"])
                for event in index[index["kind"] == 5]:
                    if event["crc_error"]:
                        continue
                    board = mapping[(int(event["chain"]), int(event["node"]))]
                    observations.append((board, services[int(event["kind_row"])]))
        selected = None
    else:
        journal = folder / "evidence.jsonl"
        artifact = folder / manifest.get("artifact", {}).get("name", "")
        if journal.is_file():
            records = [json.loads(line) for line in journal.read_text(encoding="utf-8").splitlines()]
        if artifact.is_file() and artifact.suffix == ".h5":
            with h5py.File(artifact, "r") as f:
                if not records and "evidence/records_jsonl" in f:
                    records = [json.loads(line) for line in text(f["evidence/records_jsonl"]).splitlines()]
                if "telemetry/samples" in f:
                    for row in f["telemetry/samples"][:]:
                        if not row["crc_error"]:
                            observations.append((int(row["board"]), row))
        audit = next((r["configuration"].get("configuration_audit", {})
                      for r in records if r["type"] == "configuration"), {})
        selected = int(manifest["board"])
        for record in records:
            if record["type"] == "configuration":
                per_channel_names.update(a["Name"] for a in record["configuration"]["requested_assignments"]
                                         if a.get("Channel") is not None)

    # The audit resolves board overrides. A common setting may apply only to
    # boards without their own override; do not select the first textual match.
    configuration, common = {}, {}
    for setting in audit.get("settings", []):
        if (setting["name"] not in PARAMETERS or setting["name"] in per_channel_names
                or setting["status"] != "applied"):
            continue
        for value in setting.get("effective", []):
            target = (configuration.setdefault(str(value["board"]), {})
                      if "board" in value else common)
            target[setting["name"]] = value["value"]
    if selected is not None:
        configuration = {str(selected): configuration.get(str(selected), {})}

    telemetry = {}
    for board, row in observations:
        values = telemetry.setdefault(str(board), {field: [] for field in FIELDS})
        for field, bit in FIELDS.items():
            if int(row["validity"]) & (1 << bit):
                values[field].append(float(row[field]))
    ranges = {board: {field: {"samples": len(values), "min": min(values), "max": max(values)}
                      for field, values in fields.items() if values}
              for board, fields in telemetry.items()}
    output = {
        "id": manifest.get("run_id", manifest.get("scan_id")),
        "manifest_schema_version": manifest["schema_version"],
        "started_at_utc": manifest["started_at"],
        "applied_baseline_by_board": configuration,
        "common_settings": common,
        "telemetry_ranges_by_board": ranges,
    }
    if per_channel_names & PARAMETERS:
        output["per_channel_parameters_omitted"] = sorted(per_channel_names & PARAMETERS)
    if normal:
        output.update(total_decoded_events=int(manifest["event_count"]),
                      spectroscopy_events=spectroscopy_events,
                      service_rows=len(observations))
    else:
        output.update(scan_request=manifest["request"],
                      completed_points=manifest["completed_points"],
                      restored=manifest["restored"],
                      override_records=[r for r in records if r["type"] == "overrides"],
                      evidence_errors=[r for r in records if r.get("error")])
        if not records:
            output["note"] = "No saved configuration evidence: do not infer it from a nearby run."
        elif not observations:
            output["note"] = "No HDF5 telemetry table found; inspect service/monitor records in evidence.jsonl."
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    print(json.dumps(summarize(args.directory), indent=2))


if __name__ == "__main__":
    main()
