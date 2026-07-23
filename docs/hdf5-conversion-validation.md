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

Production manifests and the embedded `/run/manifest_json` snapshot also carry
configuration SHA-256 identities, discovered board topology and firmware
evidence, DAQ VCS/dirty/Go identity, storage format/writer/compression identity,
and acquisition queue, backpressure, raw-capture, journal, and histogram
settings. `/run/metadata_json` retains the same bounded identity at file
creation time. Unknown DT5215 product and firmware values are represented as
nullable values with `unknown-not-queried` evidence.

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

## Blosc LZ4 level 4 with bit-shuffle trial

The pictured configuration is available experimentally by setting:

```text
PET_CAEN_HDF5_COMPRESSION=blosc-lz4-level4-bitshuffle
```

It applies HDF5 filter 32001 to every chunked event dataset with Blosc
parameters `clevel=4`, `shuffle=bit-shuffle`, and `compressor=lz4`. The file
records that choice in `/run/compression`; the independent validator checks
the low-level filter tuple.

| Measurement | Uncompressed | LZ4-4 + bit-shuffle |
| --- | ---: | ---: |
| HDF5 size | 169,966,297 B | 61,493,351 B |
| Fraction of JSONL source | 25.15% | 9.10% |
| Fraction of uncompressed HDF5 | 100% | 36.18% |
| Conversion wall time, including `go run` | 23.22 s | 37.09 s |
| Independent full validation | 0.39 s | 0.68 s |
| Read all 5,605,003 `high_gain` values | 0.067 s | 0.161 s |

The energy-column sums matched exactly (`16,579,091,493`). This setting saves
63.82% relative to typed uncompressed HDF5, while this first measurement made
conversion about 60% slower and the full energy-column read about 2.4 times
slower. It remains opt-in until acquisition-rate and repeated cold/warm query
benchmarks establish that the write and analysis latency are acceptable.

## Direct writer throughput

`hdf5-write-benchmark` first parses and retains every decoded event in memory,
then starts the clock and appends those objects directly to the HDF5 writer.
This excludes JSON parsing and source I/O from the measured append rate.
Finalization and structural validation have separate timers.

```text
task hdf5:benchmark \
  SOURCE=/path/to/run-go-native-detector-hvon-003 \
  OUTPUT=/path/to/direct-uncompressed.h5

PET_CAEN_HDF5_COMPRESSION=blosc-lz4-level4-bitshuffle \
task hdf5:benchmark \
  SOURCE=/path/to/run-go-native-detector-hvon-003 \
  OUTPUT=/path/to/direct-compressed.h5
```

The retained run contains 87,989 events acquired over approximately ten
seconds, for an observed input rate of about 8,799 events/s. Direct-writer
measurements on 2026-07-23 were:

| Measurement | Uncompressed | LZ4-4 + bit-shuffle |
| --- | ---: | ---: |
| Direct append time | 4.036 s | 5.074 s |
| Direct append rate | 21,799 events/s | 17,343 events/s |
| Headroom over observed input rate | 2.48x | 1.97x |
| Finalize time | 0.0046 s | 0.0043 s |
| Structural validation time | 0.212 s | 0.285 s |
| Output size | 169,538,064 B | 61,201,253 B |
| Heap after preload | 93,024,016 B | 93,018,944 B |

This demonstrates that the storage writer alone can exceed the observed rate
in both modes on the benchmark host. It is not yet an end-to-end real-time
guarantee: a paced full-pipeline soak test must also include network ingest,
decode, queue contention, scheduler stalls, periodic flush behavior, and
representative slow-disk conditions.
