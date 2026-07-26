# HDF5 histogram artifact format

This document describes histogram artifact schema version 2, first observed in
the supplied captures as `pcap/run-54/run_54.histograms.h5`. It explains the
file layout, spectrum index rows, flat bin pools, histogram axes, counter
semantics, compression, and reconstruction of an individual channel spectrum.
It also contrasts version 2 with the legacy version-1 layout used by runs 48
through 50.

The schema and axis rules are **source-confirmed** by the native Go writer and
reader. The concrete run-54 dimensions and values below are
**capture-verified**.

## Purpose and lifecycle

When histogram persistence is enabled, finalization writes one run-wide
artifact:

```text
run_<run-id>.histograms.h5
```

The histogram artifact is separate from the rotated decoded-event files such
as `run_<run-id>.0000.h5`. It is a finalized snapshot covering the complete
run and is not split when the event data rotate.

The writer creates the file exclusively, writes the root metadata and
histogram datasets, changes the root `complete` attribute from `0` to `1`, and
flushes the file. A reader must reject the artifact unless `complete == 1`.
The completed artifact is hashed and registered in the run manifest.

## Root metadata

Run 54 has the following root objects:

| Object | HDF5 type | Run-54 value | Meaning |
| --- | --- | --- | --- |
| Attribute `schema_version` | scalar `uint32` | `2` | Histogram layout version |
| Attribute `complete` | scalar `uint8` | `1` | `1` means finalization completed |
| Dataset `/format` | 23 `uint8` bytes | `pet-caen-daq-histograms` | Artifact format identifier |
| Dataset `/run_id` | 2 `uint8` bytes | `54` | ASCII/UTF-8 run identifier |

`/format` and `/run_id` are byte datasets, not HDF5 string datatypes. Decode
their bytes as text:

```python
format_name = bytes(h5["format"][:]).decode("utf-8")
run_id = bytes(h5["run_id"][:]).decode("utf-8")
```

## Version-2 hierarchy

The four implemented histogram families are:

```text
/
|-- attributes
|   |-- schema_version = 2
|   `-- complete = 1
|-- format
|-- run_id
`-- histograms
    |-- pha_high
    |   |-- spectra
    |   `-- bins
    |-- pha_low
    |   |-- spectra
    |   `-- bins
    |-- toa
    |   |-- spectra
    |   `-- bins
    `-- tot
        |-- spectra
        `-- bins
```

Every enabled family has exactly two datasets:

- `spectra` is a compound index table with one row per persisted hardware
  channel.
- `bins` is a flat `uint32` pool containing all those spectra concatenated.

This parent/index plus flat-pool design avoids creating a separate HDF5 object
for every channel.

### Run-54 dimensions

| Family | `/spectra` rows | `/bins` elements | Bins per spectrum |
| --- | ---: | ---: | ---: |
| `pha_high` | 256 | 1,048,576 | 4,096 |
| `pha_low` | 256 | 1,048,576 | 4,096 |
| `toa` | 256 | 1,048,576 | 4,096 |
| `tot` | 256 | 131,072 | 512 |

Run 54 has four chains, one node per chain, and 64 channels per node:

```text
4 chains * 1 node * 64 channels = 256 spectra per family
```

The schema itself does not require exactly 256 rows. Only histograms allocated
during the run are included in the writer snapshot, so another run may have
fewer rows or omit a disabled family entirely.

## The `spectra` index table

Every `/histograms/<kind>/spectra` dataset uses this compound datatype:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `bin_offset` | `uint64` | First element of this spectrum in the family's flat `bins` dataset |
| `entries` | `uint64` | Number of input values presented to the histogram |
| `underflow` | `uint64` | Values below `minimum` |
| `overflow` | `uint64` | Values outside the upper range, plus increments that could not be stored because a `uint32` bin saturated |
| `minimum` | `float64` | Lower edge of bin zero |
| `bin_width` | `float64` | Width of each bin in the family's axis units |
| `bin_count` | `uint32` | Number of bin elements belonging to this spectrum |
| `chain` | `uint8` | DT5215 chain index |
| `node` | `uint8` | Device node on the chain |
| `channel` | `uint8` | Physical DT5202 channel, normally 0 through 63 |

`bin_offset` is an element offset, not a byte offset. The bin slice for a row
is the half-open range:

```text
bins[bin_offset : bin_offset + bin_count]
```

Rows are written in ascending `kind`, `chain`, `node`, and `channel` order.
Consumers should nevertheless locate a channel using its explicit coordinate
fields instead of assuming a particular row number. Readers reject duplicate
`(chain, node, channel)` rows within a family.

## The flat `bins` pool

`/histograms/<kind>/bins` is a one-dimensional array of little-endian
`uint32` bin counts. A value is assigned to bin `i` using:

```text
i = floor((value - minimum) / bin_width)
```

Bin `i` represents the half-open axis interval:

```text
[minimum + i * bin_width, minimum + (i + 1) * bin_width)
```

Values below `minimum` increment `underflow`. Values whose index is at or
above `bin_count` increment `overflow`. Every input increments `entries`
before range checking.

Individual bins saturate at `2^32 - 1` rather than wrapping to zero. A later
increment directed at a saturated bin increments `overflow`, while `entries`
continues to increase. In the absence of bin saturation:

```text
entries = sum(bins) + underflow + overflow
```

## Histogram families and axis units

### `pha_high` and `pha_low`

These are pulse-height spectra for decoded high-gain and low-gain energy
samples. Their axis is in raw ADC channel codes, not calibrated energy such as
keV.

```text
minimum   = 0
bin_width = configured_ADC_channel_count / configured_energy_bin_count
```

The supported ADC domains are:

- 13-bit range: 8,192 ADC channels, codes 0 through 8,191;
- 14-bit range: 16,384 ADC channels, codes 0 through 16,383.

Run 54 uses 4,096 bins with width 2, therefore each bin combines two ADC
codes and covers the 13-bit interval `[0, 8192)`.

### `toa`

The time-of-arrival axis is stored in nanoseconds. One decoded ToA tick is
source-confirmed as 0.5 ns:

```text
minimum   = configured ToAHistoMin, in ns
bin_width = 0.5 ns * configured ToARebin
```

Run 54 has `minimum = 0`, `bin_width = 0.5`, and 4,096 bins, covering
`[0, 2048)` ns.

### `tot`

The time-over-threshold histogram uses the native 9-bit ToT code domain,
0 through 511. The persisted axis is in raw ToT codes; it is not converted to
nanoseconds by the histogram accumulator.

```text
minimum   = 0
bin_width = 512 / configured_ToT_bin_count
```

Run 54 uses 512 bins with width 1, so each bin corresponds to one native ToT
code and covers `[0, 512)`.

## Detailed run-54 example

The first `pha_high` spectrum row is:

| Field | Value |
| --- | ---: |
| `bin_offset` | 0 |
| `entries` | 384,679 |
| `underflow` | 0 |
| `overflow` | 316 |
| `minimum` | 0 |
| `bin_width` | 2 |
| `bin_count` | 4,096 |
| `chain` | 0 |
| `node` | 0 |
| `channel` | 0 |

This identifies the high-gain PHA spectrum for chain 0, node 0, channel 0.
Its counts are:

```text
/histograms/pha_high/bins[0:4096]
```

Bin 0 covers ADC codes `[0, 2)`, bin 1 covers `[2, 4)`, and bin 4095 covers
`[8190, 8192)`. During the run, 384,679 high-gain samples were presented to
this histogram. None fell below zero, and 316 were above the configured range
or encountered a saturated bin.

The next row is chain 0, node 0, channel 1:

| Field | Value |
| --- | ---: |
| `bin_offset` | 4,096 |
| `entries` | 384,306 |
| `underflow` | 0 |
| `overflow` | 361 |
| `minimum` | 0 |
| `bin_width` | 2 |
| `bin_count` | 4,096 |

Its half-open bin slice is therefore:

```text
/histograms/pha_high/bins[4096:8192]
```

The first ToA row for chain 0, node 0, channel 0 has 1,191 entries,
`minimum = 0`, `bin_width = 0.5`, and 4,096 bins. Its bin `i` corresponds to:

```text
[i * 0.5 ns, (i + 1) * 0.5 ns)
```

The corresponding ToT row also has 1,191 entries, but uses 512 bins of width
one native ToT code.

## Reconstructing one spectrum

Conceptually, a reader performs these steps:

1. Check `complete == 1`.
2. Check the format identifier and schema version.
3. Open `/histograms/<kind>/spectra`.
4. Find the row matching the requested `chain`, `node`, and `channel`.
5. Open `/histograms/<kind>/bins`.
6. Read `bin_count` elements starting at `bin_offset`.
7. Construct axis edges from `minimum` and `bin_width`.

Example using `h5py`:

```python
import h5py
import numpy as np

path = "pcap/run-54/run_54.histograms.h5"
kind = "pha_high"
wanted = (0, 0, 0)

with h5py.File(path, "r") as h5:
    if int(h5.attrs["complete"]) != 1:
        raise ValueError("incomplete histogram artifact")
    if int(h5.attrs["schema_version"]) != 2:
        raise ValueError("unsupported histogram schema")

    rows = h5[f"histograms/{kind}/spectra"][:]
    matches = [
        row for row in rows
        if (int(row["chain"]), int(row["node"]), int(row["channel"])) == wanted
    ]
    if len(matches) != 1:
        raise ValueError(f"expected one spectrum, found {len(matches)}")

    row = matches[0]
    start = int(row["bin_offset"])
    stop = start + int(row["bin_count"])
    counts = h5[f"histograms/{kind}/bins"][start:stop]

    minimum = float(row["minimum"])
    width = float(row["bin_width"])
    edges = minimum + np.arange(len(counts) + 1) * width
```

The default files use the HDF5 Blosc/LZ4 filter. Python environments may need
an HDF5 Blosc filter plugin, commonly loaded by importing the appropriate
plugin package before opening the file. Metadata may remain visible without
the plugin, but reading compressed dataset contents requires filter support.

The native Go historical reader reads only the selected channel's bin range
using an HDF5 hyperslab; it does not load the complete flat bin pool for every
request.

## Missing or quiet channels

Histogram arrays are allocated lazily when the first qualifying sample arrives.
Consequently, a completed artifact may have no `spectra` row for a quiet
channel. Absence means that no persisted histogram was allocated for that
coordinate; it is not a malformed offset.

The application reader uses the immutable run configuration from the manifest
to return a correctly sized zero-filled spectrum for a requested missing
channel. A standalone HDF5 reader that does not also read the manifest cannot
infer every disabled or quiet channel solely from the rows present in the
artifact.

## Compression and storage

Both `bins` and `spectra` are extensible, chunked datasets. The default
histogram compression is Blosc/LZ4, controlled by
`PET_CAEN_HDF5_COMPRESSION`; compression can also be explicitly disabled.

In run 54:

- `pha_high/bins` uses 16,384-element chunks and compresses 4 MiB of logical
  `uint32` data to approximately 498 KiB;
- `tot/bins` uses the same chunk length and compresses 512 KiB of logical data
  to approximately 8 KiB;
- the complete artifact occupies approximately 798 KiB on disk.

Compression ratios depend on histogram contents and should not be treated as
fixed format properties.

## Difference from schema version 1

Runs 48 through 50 use the legacy layout:

```text
/histograms/pha_high_<chain>_<node>_<channel>
/histograms/pha_low_<chain>_<node>_<channel>
/histograms/toa_<chain>_<node>_<channel>
/histograms/tot_<chain>_<node>_<channel>
```

For a complete four-board run this creates:

```text
4 families * 4 chains * 64 channels = 1,024 HDF5 datasets
```

Each legacy dataset contains only one channel's bins and carries scalar
attributes such as entries, underflow, and overflow. Version 2 replaces those
1,024 datasets and thousands of scalar attributes with eight datasets: one
`spectra` table and one `bins` pool for each of four families.

| Property | Version 1 | Version 2 |
| --- | --- | --- |
| Channel identity | Encoded in dataset name | Explicit `chain`, `node`, `channel` fields |
| Bin storage | One dataset per channel | One concatenated pool per family |
| Axis/count metadata | Dataset attributes | One compound `spectra` row |
| Full four-board object count | 1,024 histogram datasets | 8 histogram datasets |
| Channel read | Open named dataset | Find row, then read a hyperslab |
| Quiet channels | Dataset may be absent | Spectrum row may be absent |

The production historical reader remains backward-compatible. It detects
version 2 by the presence of `/histograms/<kind>/spectra`; otherwise it falls
back to the version-1 per-channel dataset name.

The observed run-50 legacy artifact is approximately 3.9 MiB, while the run-54
version-2 artifact is approximately 798 KiB. These are different runs with
different histogram contents, so the sizes illustrate the result but are not a
controlled compression benchmark.

## Command-line inspection

Show the complete hierarchy, datatypes, and root attributes:

```sh
h5dump -H pcap/run-54/run_54.histograms.h5
```

Show storage layout, chunks, and filters:

```sh
h5dump -pH -d /histograms/pha_high/bins \
  pcap/run-54/run_54.histograms.h5
```

Show the first three PHA-high spectrum rows:

```sh
h5dump -d /histograms/pha_high/spectra -s 0 -c 3 \
  pcap/run-54/run_54.histograms.h5
```

Show the 4,096 bins belonging to the first PHA-high row:

```sh
h5dump -d /histograms/pha_high/bins -s 0 -c 4096 \
  pcap/run-54/run_54.histograms.h5
```

When implementing an independent reader, validate offsets and counts before
forming a slice:

```text
bin_offset <= length(bins)
bin_count  <= length(bins) - bin_offset
```

Also reject duplicate coordinates and unsupported schema versions rather than
silently choosing an arbitrary spectrum.
