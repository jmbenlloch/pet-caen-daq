# Acquisition backpressure and batched HDF5 report

Status: measured on real runs 44 and 48 on 2026-07-25

## Summary

The original live-storage path could not keep pace with the DT5215 stream.
Each spectroscopy event independently extended and wrote the energy, timing,
parent, and run-wide index HDF5 datasets, and the run writer performed a file
size `stat` after every event. A 32-batch pipeline therefore filled, blocked
the TCP reader, and propagated backpressure to the concentrator.

The revised path:

- gives the packaged Docker server and local Compose service a 512-batch
  pipeline;
- passes each decoded acquisition batch to storage as one ordered group;
- accumulates up to eight acquisition batches or 16 MiB of event payload;
- appends consecutive spectroscopy energy, timing, parent, and index rows with
  one dataset operation per table and group;
- checks HDF5 segment size once per buffered flush instead of per event.

The HDF5 writer remains serial and preserves descriptor order. Buffering is
bounded independently of the DT5215 maximum accepted wire batch of 66 MiB plus
its 12-byte header.

## Measurements

| Measurement | Run 44, old path | Run 48, batched path |
| --- | ---: | ---: |
| Requested acquisition | 15 s | 15 s |
| Persisted events | 75,444 | 986,614 |
| Raw batches | 1,054 | 7,276 |
| Stream bytes | 22.8 MB | 293.2 MB |
| Stream delivery span | 27.874 s | 15.701 s |
| Average stream throughput | 0.818 MB/s | 18.676 MB/s |
| Peak one-second delivery | 3.345 MB | 20.891 MB |
| Pipeline capacity | 32 | 512 |
| Last stream byte to termination evidence | 317 ms | 109 ms |
| Journal termination to finalization start | about 7.5 s | about 2 ms |

Run 44's packet capture contains 63 zero-window advertisements from the DAQ
host. That is direct evidence that application consumption stalled until the
kernel receive buffer filled. Run 48 remained within a few batches of fully
caught up during acquisition: the observed live snapshot showed three batches
of total backlog, ingress depth 2/512, raw worker depth zero, event worker depth
two, and no rejects or failures.

Run 48 has no evidence of duplicated storage:

- `wire.raw` contains 7,276 CRC-valid records and 986,614 descriptors;
- the two HDF5 index datasets contain 861,792 and 124,822 rows;
- their sum and the manifest event count are both 986,614;
- 986,558 descriptors are spectroscopy events and 56 are service events;
- no descriptor has its CRC-error flag set.

The higher event count is therefore genuine. Removing host backpressure allowed
the concentrator to deliver substantially more of the board output during the
same acquisition interval.

## Trigger-ID gaps

Run statistics currently derive `lost_trigger_count` from gaps between
persisted trigger IDs. The active configuration uses `TrgIdMode TRIGGER_CNT`,
which counts triggers arriving at the board whether or not they become accepted
and recorded events. These gaps are useful evidence but are not, by themselves,
proof of TCP, decoder, or storage loss. Operator presentation should call them
`trigger-ID gaps` until a hardware counter with narrower loss semantics is
available.

## Remaining stop latency

Run 48 proved that the acquisition pipeline itself can be almost empty at
stop. Remaining operator-visible latency can still occur after the hardware
stops because the synchronous finalization path may:

1. stop the read loop;
2. send `ACQ_STOP` and wait for the 100 ms idle drain condition;
3. wait for accepted pipeline work;
4. snapshot and write all histogram datasets;
5. flush and close raw capture and the transport journal;
6. flush and close the current HDF5 segment;
7. read every artifact again to calculate SHA-256;
8. publish the final manifest and transition to ready.

The instrumented build emits one `run_timing` log record for every stage above,
including per-artifact hash time and byte count. Capture the server log from
the next representative run with:

```sh
docker logs --timestamps <daq-container> 2>&1 | grep 'run_timing'
```

The `ready total_stop_ms` record is the operator-visible stop latency. Compare
its component durations before changing durability or integrity behavior.
