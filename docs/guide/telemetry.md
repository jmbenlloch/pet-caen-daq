# Reading telemetry

The backend publishes complete snapshots. After reconnection, the browser replaces
its prior state with the first complete snapshot. Backend connectivity and fresh
board measurements are separate: an online backend does not establish that every
board measurement is current.

## Freshness

The interface marks the backend offline/stale after five seconds without a fresh
snapshot and retries a broken stream every two seconds. Check the board's telemetry
update time as well as the backend connection indicator. A missing or stale reading
must not be interpreted as a zero measurement.

## Rates and counters

Live rates are differences between the latest two usable snapshots divided by the
elapsed interval. Historical rates are averages over the completed run. Compare
values with the same source, board selection, and time interval.

Estimated lost triggers come from forward trigger-ID gaps. They are diagnostic
estimates, rather than a direct hardware loss counter; resets and acquisition mode
matter when interpreting them.

Channel discriminator rates, board event counts, and stored event totals describe
different stages of acquisition. Do not assume their values should be identical.

See the [complete operator manual](../operator-user-manual.md) for workspace fields,
run statistics, storage progress, and historical data selection. Temperature and
voltage measurements are explained in [temperatures and HV](temperatures-and-hv.md).
