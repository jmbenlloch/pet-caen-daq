# HDF5 conversion and validation

The production toolchain builds `bin/jsonl-to-hdf5`. Convert a finalized
development run without modifying its source:

```text
task hdf5:convert SOURCE=/path/to/run-42 OUTPUT=/path/to/run-42.h5
```

The converter streams `events.jsonl`, requires contiguous sequence numbers,
checks envelope/payload kinds and typed descriptor identity, and verifies the
converted count against `manifest.json`. It embeds the source configuration
and manifest snapshots, finalizes the HDF5 file, and runs the Go structural
validator. Existing output paths are rejected.

For independent inspection in the pinned Docker image:

```text
python3 scripts/validate-hdf5.py /path/to/events.h5
```

Use `--allow-incomplete` only for evidence inspection. It relaxes the
completion-marker requirement but still checks physical compound fields,
event routing, child bounds, and parent identity.

## Retained real-run baseline

On 2026-07-23, `run-go-native-detector-hvon-003` was converted through the
Docker toolchain:

| Measurement | Result |
| --- | ---: |
| JSONL source size | 675,772,115 bytes |
| HDF5 output size, uncompressed | 169,966,297 bytes |
| Output/source ratio | 25.15% |
| Wall time, including `go run` startup | 23.22 s |
| Independent h5py validation | 0.39 s |
| Run-wide/spectroscopy events | 87,989 |
| Energy child rows | 5,605,003 |
| Timing child rows | 946,716 |

Both the Go validator and Python/h5py validator accepted the output. The
smaller file is due to typed binary representation; production datasets do not
yet enable compression. Peak memory, direct prebuilt-binary throughput,
representative analysis-query latency, and compressed alternatives still need
controlled benchmarks before choosing final chunk/compression defaults.
