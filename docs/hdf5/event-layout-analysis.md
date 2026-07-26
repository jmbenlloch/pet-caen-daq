# Split versus flat HDF5 event layouts

## Decision

For now, only spectroscopy events will use the new schema-v2 observation
format.

The other event families retain their existing parent/child layouts:

- `/events/timing/events` and `/events/timing/hits`
- `/events/counting/events` and `/events/counting/counts`
- `/events/waveform/events` and `/events/waveform/samples`
- `/events/service/events`, `/events/service/counters`, and
  `/events/service/unknown_payload`
- `/events/test/events` and `/events/test/words`

This decision is based on Go benchmarks using the same HDF5 binding,
compression library, filter parameters, and chunk size as the backend.
Spectroscopy is the only measured event family for which flattening has a
large, demonstrated storage advantage that offsets the additional write cost.

## Question investigated

The original decoded-event schema separates event-level data from variable
numbers of child measurements. For example, a timing event owns a slice of
rows in a timing-hit table.

That representation is compact, but analysis must combine:

1. `/events/index`, which identifies the source and global event sequence;
2. the event-family parent table; and
3. one or more child tables.

The proposed flat representation repeats the source and event fields in every
child observation. It is easier to load directly into pandas, Polars, ROOT, or
another analysis system, but may consume more storage and CPU.

The schema-v2 spectroscopy design is actually a hybrid:

- the compact `/events/spectroscopy/events` parent remains, preserving event
  boundaries and `/events/index.kind_row`; and
- separate energy and timing children are replaced by the self-contained
  `/events/spectroscopy/observations` table.

An empty spectroscopy event receives one sentinel observation so that reading
only the observation table does not silently discard the event.

Applying the same design to another family therefore means retaining its
compact parent table as well as writing the new observation table.

## Benchmark implementation

The Go benchmark is located at:

```text
backend/cmd/hdf5-layout-benchmark
```

It reads decoded HDF5 datasets using Go and writes two alternatives:

- `split`: the current parent and child datasets;
- `flat`: a self-contained observation table with repeated index, source, and
  event fields.

The measurements used:

| Setting | Value |
|---|---|
| HDF5 binding | The backend's `github.com/next-exp/hdf5-go` dependency |
| Blosc | 1.21.6, dated 2024-06-24 |
| Codec | LZ4 |
| Compression level | 4 |
| Shuffle | Bitshuffle |
| Chunk length | 16,384 rows |
| Implementation language | Go |
| Repetitions | Three per layout |

Reported times are the medians of the three repetitions. File sizes were
deterministic across repetitions.

The benchmark outputs contain only the event-family datasets under test. They
exclude `/configuration`, `/run`, histograms, and unrelated event families.
Consequently, percentages describe the affected datasets rather than the
percentage change of an entire production artifact.

Unless stated otherwise, the reported flat file contains only the observation
table. It is therefore an optimistic lower bound for the hybrid schema, which
also retains the compact parent table.

## Real-data sources

The existing event-format reports were used to identify the capture-verified
samples:

| Event family | File | Parent rows | Child rows |
|---|---|---:|---:|
| Timing | `pcap/run-2/run_2.0000.h5` | 1,608,067 | 19,704,749 hits |
| Waveform | `pcap/run-70/run_70.0000.h5` | 65,263 | 52,210,400 samples |
| Service | `pcap/run-54/run_54.0000.h5` | 60 | 2,903 counters |
| Counting | `pcap/run-61/run_61.0000.h5` | 7 | 141 counters |
| Test | No populated retained capture | 0 | 0 |

Run 70 has a second waveform segment, but its first segment already supplies
more than 52 million real sample rows and is large enough to characterize the
layout.

No `pcap/run-*` decoded HDF5 file contains a populated
`/events/test/events` dataset. Test-layout measurements are therefore
explicitly synthetic and are not presented as capture evidence.

Run 61 contains real counting data, but only seven events. A second,
clearly-labeled scaled fixture was used to separate HDF5 fixed overhead from
the behavior of the row layout.

## Summary results

The following sizes include only the datasets written by each benchmark
alternative:

| Family and evidence | Split size | Flat observation-only size | Flat size change | Median split time | Median flat time | Time change |
|---|---:|---:|---:|---:|---:|---:|
| Timing, real run 2 | 159,875,867 B | 203,661,275 B | +27.4% | 3.134 s | 5.926 s | +89% |
| Waveform, real run 70 | 525,169,181 B | 573,114,635 B | +9.1% | 4.933 s | 8.793 s | +78% |
| Service counters, real run 54 | 27,859 B | 48,640 B | +74.6% | 0.00366 s | 0.110 s | about 30 times |
| Counting, real run 61 | 13,986 B | 11,382 B | −18.6% | 0.00578 s | 0.00500 s | fixed overhead dominates |
| Counting, scaled run-61 shape | 3,435,516 B | 2,454,762 B | −28.5% | 0.0575 s | 0.338 s | about 5.9 times |
| Test, synthetic fixture shape | 2,368,753 B | 20,112,189 B | +749% | 0.0442 s | 0.288 s | about 6.5 times |

The real run-61 result is too small to support a production storage
conclusion. Its files are dominated by dataset headers, chunks, and filter
metadata.

The scaled counting flat size is also optimistic because it excludes the
parent table that the hybrid schema would retain.

## Timing analysis

Run 2 contains:

| Quantity | Rows |
|---|---:|
| Timing events | 1,608,067 |
| Timing hits | 19,704,749 |
| Zero-hit timing events | 1,442,667 |

Approximately 89.7% of the timing events have no child hit. A one-table layout
that preserves every event therefore needs 1,442,667 sentinel rows.

The observation-only flat result was already 27.4% larger and 89% slower than
the current split representation. The retained compact timing parent table
uses another 17,924,152 allocated bytes. An actual hybrid layout would
therefore use approximately 221.6 MB for these two datasets, roughly 38.6%
more than the current 159.8 MB split layout.

The current representation also captures an important distinction cleanly:
an event can exist even when it has no accepted timing hit.

### Timing decision

Keep timing split.

A future analysis API may expose enriched, self-contained hit rows while
returning zero-hit events separately. That would simplify analysis without
requiring sentinel rows in the physical file.

## Waveform analysis

Every real run-70 waveform event in the measured segment contains 800 samples.
Flattening repeats sequence, trigger ID, timestamp, chain, node, and qualifier
800 times per event.

Bitshuffle and LZ4 compress the repeated columns effectively: the logical
uncompressed flat data is approximately twice as large, while the compressed
file is only 9.1% larger. Nevertheless, this still adds approximately 48 MB to
the measured segment and increases rewrite time by approximately 78%.

The retained waveform parent table adds another 297,361 allocated bytes. It is
small relative to the samples, but makes the hybrid result slightly worse
than the observation-only result in the table.

Waveform parent/child reconstruction is also simple:

```python
parent = events[parent_row]
samples = sample_table[
    parent["sample_offset"]:
    parent["sample_offset"] + parent["sample_count"]
]
```

### Waveform decision

Keep waveform split.

Repeating event metadata for every sample has a real storage and CPU cost, and
the current fixed-size sample slices are straightforward to read.

## Service analysis

Service events are not simply a parent plus homogeneous observations. They
contain:

1. one telemetry/status record;
2. zero or more decoded channel counters; and
3. zero or more arbitrary, undecoded raw bytes.

The benchmark flattened the decoded counters by repeating all telemetry and
source fields for each counter row. It deliberately kept unknown bytes out of
that table. Duplicating an arbitrary raw payload in every channel row would
not be a sensible or lossless table design.

Even this favorable counter-only comparison made the flat data 74.6% larger.
Fourteen of the 60 real service events had no counter and therefore required
sentinel rows.

The reported service files exclude the identical unknown-byte pool from both
alternatives. Adding it to both would not reverse the result. An actual hybrid
would also retain the service parent table.

The approximately 30-times timing ratio is not operationally important by
itself because both files are tiny, but there is no storage or structural
advantage to compensate for it.

### Service decision

Keep service split.

Its telemetry, counters, and unknown bytes have different semantics and should
remain separate physical datasets.

## Counting analysis

The seven real run-61 events contain 141 total counter rows. The flat file is
slightly smaller only because it replaces two very small datasets with one;
fixed HDF5 overhead dominates the measurement.

The scaled fixture preserved the observed approximate shape:

| Quantity | Value |
|---|---:|
| Counting events | 100,000 |
| Counters per event | 20 |
| Counter observations | 2,000,000 |

At that scale, repeated event metadata compressed well. The observation-only
flat file was 28.5% smaller, but took approximately 5.9 times longer to write.
The hybrid schema would additionally retain the compact parent table, reducing
or possibly eliminating the size advantage.

Counting events are normally much less frequent than spectroscopy or waveform
events, so the absolute CPU requirement may still be acceptable. Counting is
the only additional family where flattening might eventually be justified for
analysis ergonomics.

### Counting decision

Keep counting split for schema v2.

Before reconsidering it, collect a substantially longer real counting run and
measure the actual streaming writer rather than only an offline rewrite. The
decision should be based on the final hybrid size, acquisition CPU utilization,
and event throughput.

## Test-event analysis

There is no populated real test-event HDF5 capture. The test benchmark used a
synthetic fixture based on the implemented decoder fixtures:

| Quantity | Value |
|---|---:|
| Test events | 500,000 |
| Words per event | 2 |
| Total words | 1,000,000 |

The words varied between events to avoid measuring an unrealistically
compressible constant payload.

The current layout stores test words in a packed `uint32` dataset. The flat
compound table interleaves each word with repeated sequence, source, trigger,
and timestamp fields. It was approximately 8.5 times larger and 6.5 times
slower to write.

Because this is synthetic evidence, the exact ratio must not be generalized to
all possible test payloads. The structural result is still useful: arbitrary
test words are best kept in a dense primitive array rather than interleaved
with repeated metadata.

### Test decision

Keep test events split unless a populated real capture demonstrates a
different workload.

## Why spectroscopy is different

Spectroscopy flattening does more than repeat parent metadata. It replaces two
child tables:

- energy observations; and
- timing observations.

Energy and timing entries for the same `(event, channel)` become one row.
Consequently, flattening can remove rows as well as remove a join.

The previous Go spectroscopy benchmark measured:

| Layout | File size |
|---|---:|
| Current split spectroscopy layout | 467,239,246 B |
| Flat self-contained observations | 185,556,139 B |

The flat result was approximately 60.3% smaller. It required approximately 3.2
times more rewrite time, but the storage reduction and much simpler analysis
model are substantial.

That combination was not reproduced by timing, waveform, service, or test.
Counting remains uncertain rather than clearly beneficial.

## Final recommendation

Use the new observation schema only for spectroscopy:

```text
/events/spectroscopy/events
/events/spectroscopy/observations
```

Do not interpret HDF5 schema version 2 as requiring every event family to have
the same physical layout. A schema can use the most appropriate representation
for each event family while providing a uniform analysis API above it.

For users who want flat tables, provide reader helpers that materialize or
stream enriched rows:

- join timing hits with timing parents on demand;
- attach counting metadata to counter rows on demand;
- attach waveform metadata to sample blocks on demand;
- return service telemetry, counters, and raw payloads as separate typed
  objects; and
- expand test words only when requested.

This preserves efficient acquisition storage without forcing every downstream
analysis to implement offset handling manually.
