# PET CAEN DAQ

## Operator User Manual

**Document type:** User and operations manual<br>
**Applies to:** `pet-caen-daq` web operator interface and Go acquisition service<br>
**Hardware:** One CAEN DT5215 concentrator and four CAEN DT5202 front-end boards<br>
**Document revision:** 1<br>
**Revision date:** 2026-07-25

---

## Purpose of this manual

This manual describes how to install, start, operate, monitor, and recover the PET CAEN data-acquisition system. It explains every function offered by the web frontend, the conditions under which each control is available, and the meaning of the information displayed to the operator.

The manual is intended both for human operators and as a retrieval source for an AI-assisted help system. For that reason, controls are referred to by their exact visible labels, important terms are defined where they first appear, and common questions and failure cases are described explicitly.

This manual describes the behavior implemented in the repository on the revision date. It follows the organization and technical-manual style of the CAEN _Janus 5202 User Manual_, while documenting this application rather than the JANUS desktop program.

## Safety notice

The DAQ controls detector bias voltage and changes DT5202 acquisition registers. Incorrect values can damage a detector, invalidate data, or leave hardware in an unsafe state.

> **WARNING — HIGH VOLTAGE**
>
> Only trained personnel may enable SiPM high voltage. Confirm the detector, cabling, configured bias, current limit, cooling, and site safety procedure before selecting **All HV on** or a board-level **Turn board … HV on** button.

> **WARNING — EXCLUSIVE HARDWARE CONTROL**
>
> Do not run JANUS, FERSlib utilities, another DAQ instance, or any other control client against the DT5215 while this system is connected. Concurrent clients can interleave commands and make the displayed state unreliable.

> **CAUTION — PRESERVE FAULT EVIDENCE**
>
> If a run fails, do not delete or edit its run directory. An incomplete marker, raw capture, transport journal, and manifest can be needed to determine what happened.

The software applies several safety gates:

- run start, scans, and HV switching are accepted only while the system is **Ready**;
- HV-on additionally requires the backend to have been started with `-authorize-hv-config`;
- scans cannot overlap an acquisition run, configuration operation, or another scan;
- a run is not finalized until acquisition has stopped and accepted buffered data has drained to storage;
- configuration values are validated in both the browser and backend;
- a failed multi-board HV-on operation attempts to turn off boards that were already enabled by that operation;
- the DAQ validates the provisioned four-link topology but does not alter persistent DT5215 link enablement.

These gates supplement, but do not replace, operator review and site procedures.

## Symbols, abbreviated terms, and notation

| Term           | Meaning                                                                    |
| -------------- | -------------------------------------------------------------------------- |
| ADC            | Analog-to-digital converter                                                |
| ASIC           | Application-specific integrated circuit; the DT5202 uses CITIROC devices   |
| DAQ            | Data acquisition                                                           |
| DT5202         | 64-channel front-end board                                                 |
| DT5215         | FERS concentrator used to control and receive data from the DT5202 boards  |
| FERS           | Front-End Readout System                                                   |
| HG / LG        | High gain / low gain                                                       |
| HV             | High voltage or detector bias voltage                                      |
| Imon           | Monitored HV output current                                                |
| PHA            | Pulse-height analysis                                                      |
| QD / Q-OR      | Charge discriminator / logical OR of charge discriminator activity         |
| run            | A numbered acquisition or diagnostic scan stored in the common run catalog |
| SiPM           | Silicon photomultiplier                                                    |
| TD / T-OR      | Timing discriminator / logical OR of timing discriminator activity         |
| TDlink         | Optical/electrical link between the DT5215 and a front-end chain           |
| ToA / ToT      | Time of arrival / time over threshold                                      |
| Vmon           | Monitored HV output voltage                                                |
| ZS             | Zero suppression                                                           |
| `B0`…`B3`      | Board identities corresponding to DT5215 chains 0 through 3                |
| `CH 0`…`CH 63` | Front-end channels on one DT5202                                           |

Board number and chain number are equivalent in the supported topology. Each enabled chain has exactly one DT5202 at node 0.

## Contents

1. [System overview](#1-system-overview)
2. [Installation and startup](#2-installation-and-startup)
3. [Operator interface overview](#3-operator-interface-overview)
4. [Connecting and disconnecting hardware](#4-connecting-and-disconnecting-hardware)
5. [Configuring an acquisition](#5-configuring-an-acquisition)
6. [Starting, monitoring, and stopping a run](#6-starting-monitoring-and-stopping-a-run)
7. [Statistics workspace](#7-statistics-workspace)
8. [Plots workspace](#8-plots-workspace)
9. [Scans workspace](#9-scans-workspace)
10. [Hardware and high-voltage workspace](#10-hardware-and-high-voltage-workspace)
11. [Run history, search, and downloads](#11-run-history-search-and-downloads)
12. [System states, diagnostics, and recovery](#12-system-states-diagnostics-and-recovery)
13. [Configuration parameter reference](#13-configuration-parameter-reference)
14. [Storage and artifact reference](#14-storage-and-artifact-reference)
15. [Recommended operating procedures](#15-recommended-operating-procedures)
16. [Troubleshooting](#16-troubleshooting)

# 1 System overview

## 1.1 Architecture

The system consists of:

1. a browser-based Vue operator interface;
2. a Go backend that owns validation, hardware communication, run control, decoding, storage, telemetry, and the run catalog;
3. one DT5215 concentrator, normally reached on TCP control port 9760 and data-stream port 9000;
4. four DT5202 boards, one at node 0 on each enabled chain 0 through 3;
5. a run directory containing manifests, event data, optional evidence files, scan datasets, and a SQLite search catalog.

The browser never writes hardware registers directly. It sends coarse operations such as “start run,” “stop run,” “set HV,” and “start scan” to the backend. The backend validates the request, checks the current state, performs the hardware sequence, and publishes complete telemetry snapshots.

## 1.2 Supported topology

Before normal operation, use the DT5215 web interface to:

- enable TDlinks 0, 1, 2, and 3;
- disable TDlinks 4, 5, 6, and 7;
- connect exactly one DT5202 at node 0 on each enabled link.

The DAQ verifies this topology. It deliberately does not change persistent TDlink enablement. A topology mismatch must be corrected through the DT5215 web interface or physical cabling.

## 1.3 Authoritative state and telemetry

The backend is the authority for system state. The browser receives independently usable snapshots containing:

- a monotonically increasing sequence number;
- the observation time;
- system and run state;
- discovered boards and health;
- HV measurements and fault flags;
- pipeline and storage counters;
- run statistics;
- active scan and configuration progress;
- diagnostics.

If no fresh snapshot is received for five seconds, the interface marks the backend offline/stale. It retries a broken telemetry connection automatically every two seconds. After reconnecting, the first complete snapshot replaces the browser's prior state; counters are not reconstructed from guesses.

## 1.4 Run identity

Data acquisitions, threshold staircases, and hold-delay scans share one monotonically increasing numeric run sequence. A number is allocated by the server; the operator does not choose it.

Run types shown in history are:

- **Data run** — normal event acquisition;
- **Staircase scan** — discriminator threshold-rate scan;
- **Hold-delay scan** — high-gain spectrum versus hold delay.

# 2 Installation and startup

## 2.1 Prerequisites

For a source build, the supported workflow uses:

- Go;
- Node.js and the pinned frontend dependencies;
- Task;
- Buf and generated ConnectRPC bindings;
- HDF5 and Blosc for the production HDF5 build;
- Docker for the reproducible toolchain and local simulator workflow.

Use repository Task targets rather than assembling production commands from individual build tools.

## 2.2 Read-only hardware acceptance

Before the first controlled run, after a concentrator/cabling change, or after an unexplained hardware fault:

1. stop JANUS and all other control clients;
2. confirm the DT5215 web-interface provisioning described in Section 1.2;
3. preserve the configuration that will be inspected;
4. run:

   ```sh
   task hardware:inspect CONFIG=path/to/config.txt
   ```

The inspection opens the control and stream connections and performs chain-information and register-read operations only. It does not bind the HTTP server, create run storage, reset or enumerate links, synchronize boards, issue acquisition commands, write registers, or read the HV peripheral through selector writes.

### Discovering connected cards from the dashboard

When the backend is online and the hardware is disconnected, select **Discover
cards** in the connection controls. Discovery opens a temporary DT5215
connection, resets and enumerates every TDlink enabled in the DT5215 web
interface, synchronizes those links, and reads the product ID, FPGA firmware,
and acquisition status of every enumerated node. The resulting chain/node cards
remain visible for review after the temporary connection closes.

The **Connect** section of the Configuration panel then lists the proposed
`Open[board]` address for every discovered card in physical `(chain, node)`
order. Select **Use discovered addresses** to replace the document's existing
indexed `Open` entries. This is an explicit editor change: review or save the
configuration before connecting, and use the raw-configuration view if an
external detector-position convention requires different logical board
numbers.

Discovery does not enable persistent TDlinks, apply the JANUS configuration,
change high voltage, or start acquisition. It is nevertheless a state-changing
hardware operation because it resets, enumerates, and synchronizes enabled
links. Stop acquisition and disconnect the normal hardware session before
using it. The discovered order is physical `(chain, node)` order; it does not
infer detector position or rewrite the logical `Open[board]` mapping.

A successful inspection:

- lists the DT5215 identity;
- lists four DT5202 boards;
- confirms they are ready and not running;
- ends with `inspection complete mode=read-only hardware_writes=0`.

If a link is not already enumerated and ready, inspection fails rather than initializing it. Correct provisioning before normal startup.

## 2.3 Local simulator system

To start a complete simulator-backed development system:

```sh
task docker:local:up
task docker:local:status
```

Open `http://localhost:5173`. The local stack contains the simulator, backend, and Vite frontend. Run data is kept in a Compose-managed volume.

Useful commands are:

```sh
task docker:local:logs
task docker:local:down
```

`docker:local:down` stops the services without deleting the run-data volume.

## 2.4 Source development startup

Install frontend dependencies and build:

```sh
npm --prefix frontend ci
task build
```

Start the backend with a configuration, then start the development frontend:

```sh
./bin/pet-caen-daq \
  -config path/to/config.txt \
  -control 172.16.0.11:9760 \
  -stream 172.16.0.11:9000 \
  -runs ./runs

task dev:frontend
```

Open `http://localhost:5173`. The development server proxies API calls to `127.0.0.1:8080`.

## 2.5 Single-origin production startup

Build the frontend and backend, then tell the backend where the built frontend is located:

```sh
task build
./bin/pet-caen-daq \
  -config path/to/config.txt \
  -frontend-dir frontend/dist \
  -listen 0.0.0.0:8080 \
  -runs ./runs
```

Open the backend address, for example `http://host:8080`. In this mode the UI and ConnectRPC API use the same origin.

## 2.6 Production container

Build the HDF5-enabled application image:

```sh
task container:build IMAGE=pet-caen-daq:latest
```

Example Linux launch:

```sh
mkdir -p runs
docker run --rm --network host \
  -v "$PWD/runs:/var/lib/pet-caen/runs" \
  pet-caen-daq:latest
```

The container runs as numeric user `65532`; a bind-mounted run directory must be writable by that user. Docker Desktop normally needs `-p 8080:8080` instead of host networking and must be able to route to the hardware subnet.

The published production image includes a sample configuration and enables `-authorize-hv-config` by default. If the container command is replaced, include that flag only when applying configured HV setpoints and enabling HV has been explicitly authorized.

## 2.7 Running the production system on Windows with Docker Desktop

The Windows operator station runs the complete application as a Docker container. The container includes the backend, built web frontend, production configuration, and HDF5 runtime. A separate frontend installation is not required.

> **WARNING — DISCONNECT JANUS FIRST**
>
> JANUS and PET CAEN DAQ must never control the DT5215/DT5202 system at the same time. The PET CAEN DAQ container attempts to connect to the hardware when it starts. Before running or restarting the container, stop any JANUS acquisition, disconnect JANUS from the hardware, and close JANUS. Also make sure no other FERS or DAQ utility is connected.

### First launch

Open PowerShell and run:

```powershell
docker run -d `
    --name pet-caen-daq `
    -p 8081:8080 `
    -v "C:\Users\investigator\daq\docker:/var/lib/pet-caen/runs" `
    nextmgmt/pet-caen-daq:latest
```

The options have the following meaning:

| Option                                                         | Meaning                                                                                                                                             |
| -------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docker run -d`                                                | Starts the application in the background.                                                                                                           |
| `--name pet-caen-daq`                                          | Assigns the reusable container name `pet-caen-daq`.                                                                                                 |
| `-p 8081:8080`                                                 | Publishes container port 8080 as Windows port 8081.                                                                                                 |
| `-v "C:\Users\investigator\daq\docker:/var/lib/pet-caen/runs"` | Stores runs and the run catalog persistently in `C:\Users\investigator\daq\docker`. Data therefore remains available when the container is stopped. |
| `nextmgmt/pet-caen-daq:latest`                                 | Uses the published production image.                                                                                                                |

After the container starts, open:

<http://localhost:8081>

The browser address is port **8081**, not port 8080. Port 8080 is the private port inside the container.

The command creates a named container, so it is intended for the first launch. If a container named `pet-caen-daq` already exists, Docker reports a name conflict. Do not create a second DAQ container. After confirming that JANUS is disconnected, restart the existing container with:

```powershell
docker start pet-caen-daq
```

Useful checks are:

```powershell
docker ps --filter "name=pet-caen-daq"
docker logs pet-caen-daq
```

`docker ps` confirms whether it is running. `docker logs` shows startup, hardware-connection, configuration, and error messages.

### Connecting from the web page

When the page opens, read the masthead before issuing a command:

- **Backend online** confirms that the browser can communicate with the container.
- **Hardware connected** means the backend already owns the hardware connection.
- **Hardware disconnected** means the automatic attempt did not leave a connection; after correcting the cause, select **Connect hardware** in the upper-right operations area.
- Wait for **Ready** before starting a run, scan, or HV operation.

If the page reports a hardware connection failure, do not start JANUS to test the same hardware while the container is still connected or attempting recovery. First use **Disconnect hardware** when the button is available, then stop the container if control must be returned to JANUS.

### Ending work safely

> **CRITICAL — DISCONNECT THE HARDWARE FROM THE PAGE**
>
> Do not finish a session by closing the browser, stopping Docker, shutting down Windows, or unplugging the network alone. Before ending work, explicitly select **Disconnect hardware** in the upper-right area of the page and wait until the masthead says **Hardware disconnected**.

Use this shutdown order:

1. If a data run is active, select **Stop and drain** and wait until the system returns to **Ready**.
2. If a scan is active, allow it to complete or select **Cancel**, then wait for configuration restoration and **Ready**.
3. If detector bias must be shut down, select **All HV off** while **Ready** and confirm each board reports **HV off** and an appropriate Vmon.
4. Select **Disconnect hardware** in the upper-right operations area.
5. Wait until the page explicitly reports **Hardware disconnected**.
6. Only then stop the container:

   ```powershell
   docker stop pet-caen-daq
   ```

Closing the browser does not disconnect the backend from the DT5215. Stopping the container without using **Disconnect hardware** first prevents an orderly operator-confirmed handoff and can leave the hardware state uncertain.

JANUS may be started again only after the page has reported **Hardware disconnected** and the PET CAEN DAQ container has been stopped. Before restarting the container later, disconnect and close JANUS again.

## 2.8 Backend command-line options

| Option                 |                  Default | Purpose                                                                |
| ---------------------- | -----------------------: | ---------------------------------------------------------------------- |
| `-config`              |           none; required | JANUS-format startup configuration                                     |
| `-control`             |       `172.16.0.11:9760` | DT5215 control TCP address                                             |
| `-stream`              |       `172.16.0.11:9000` | DT5215 data-stream TCP address                                         |
| `-listen`              |         `127.0.0.1:8080` | HTTP/ConnectRPC listen address                                         |
| `-frontend-dir`        |                    empty | Built frontend directory to serve                                      |
| `-runs`                |                 `./runs` | Parent directory for run artifacts                                     |
| `-catalog`             | `<runs>/catalog.sqlite3` | SQLite run-search catalog                                              |
| `-pipeline-capacity`   |                     `32` | Bounded stream-batch ingress capacity                                  |
| `-drain-timeout`       |                     `5s` | Maximum orderly stop-and-drain time                                    |
| `-authorize-hv-config` |                    false | Authorize configured HV setpoints and HV-on                            |
| `-inspect-only`        |                    false | Validate the ready topology using read-only hardware access, then exit |

The HTTP API starts before hardware discovery. If the DT5215 cannot be reached, the UI remains available in **Disconnected** state so the operator can view stored runs, validate configurations, and retry hardware connection.

# 3 Operator interface overview

## 3.1 Masthead

The masthead is always visible and contains:

- **CAEN acquisition** — application identity;
- **Light/Dark** — switches the color theme and saves the choice in browser local storage;
- **System state** — authoritative backend state;
- **Sequence** — latest telemetry sequence number;
- **enabled links** — count of enabled DT5215 chains;
- **SiPM bias** — summary and four board indicators for HV state;
- **Backend online/offline** — freshness of the API telemetry stream;
- **Hardware connected/disconnected/connecting** — DT5215 session state;
- connection and run-control buttons.

The theme changes presentation only; it does not affect acquisition or stored data.

## 3.2 Backend and hardware indicators

**Backend online** means the browser has an active telemetry connection and received a snapshot within five seconds.

**Backend offline** can mean:

- the backend is stopped;
- the browser cannot reach it;
- the stream ended;
- snapshots are stale.

Select **Retry backend** to trigger an immediate reconnect. Automatic retries also continue in the background.

**Hardware connected** means the backend owns a DT5215 control/stream session. It does not by itself mean the system is ready to acquire; inspect the system state and diagnostics.

## 3.3 System diagnostics

Two alert areas can appear below the masthead:

- **Connection or command failed** shows browser/API command errors;
- **System diagnostics** lists backend warnings and errors as `code — message`.

Do not dismiss a diagnostic merely because a control becomes available. Record the code, message, state, board/chain if shown, and time before recovery.

## 3.4 Hardware configuration progress

When hardware configuration is active or has failed, a progress panel shows:

1. Planning;
2. Pedestal setup;
3. Register writes;
4. CITIROC configuration;
5. Register readback;
6. High voltage.

The panel can also show:

- current board out of four;
- current chain and node;
- completed operations and total operations;
- an operation-specific unit;
- an overall board progress bar;
- **Cached pedestal data reused**.

Starting a run with the same effective assignments as the last successful hardware configuration can reuse the configuration. Comments, blank lines, and source line numbers do not force a hard reconfiguration; assignment order, scope, or value changes do.

Protected-flash pedestal calibration is read and validated when first needed on a hardware connection. It can be reused for later configurations in that same session. A disconnect or backend restart creates a new session and requires fresh pedestal reads.

## 3.5 Workspace tabs

The six workspaces are:

| Tab             | Purpose                                                           |
| --------------- | ----------------------------------------------------------------- |
| **Acquisition** | Edit configuration, start/stop runs, and monitor pipeline/storage |
| **Statistics**  | Live or final board and channel counters/rates                    |
| **Plots**       | Live or persisted PHA, ToA, and ToT histograms                    |
| **Scans**       | Threshold staircase and hold-delay diagnostic scans               |
| **Hardware**    | Board identity, temperatures, HV measurements, and HV control     |
| **Runs**        | Browse, search, inspect, and download stored runs                 |

Keyboard users can move among tabs with Left Arrow, Right Arrow, Home, and End.

# 4 Connecting and disconnecting hardware

## 4.1 Connect hardware

When the backend is online and state is **Disconnected**, select **Connect hardware**.

Before selecting it, confirm that JANUS is disconnected and closed. JANUS and this DAQ cannot safely share the DT5215 connection.

Connection performs the normal runtime sequence:

1. open DT5215 control and stream transports;
2. discover and validate the four-link topology;
3. recover boards found in a stale running state when safe;
4. apply and read back the startup configuration;
5. initialize HV monitoring;
6. reach **Ready** if all required steps succeed.

The button changes to **Hardware connecting** while the operation is active. Watch configuration progress and diagnostics.

If connection fails:

1. record the error;
2. make sure no other hardware client is running;
3. verify power, network routing, addresses, cabling, and DT5215 provisioning;
4. perform read-only acceptance if the cause is uncertain;
5. select **Connect hardware** again.

## 4.2 Disconnect hardware

Select **Disconnect hardware** to close both DT5215 transports.

Disconnection is permitted only from **Idle**, **Ready**, or **Fault**. It is disabled during a run, scan, connection, configuration, start/stop, drain, or recovery. If a run is active, use **Stop and drain** first.

This button is the required final step before closing the browser, stopping the container, shutting down the operator computer, or returning hardware control to JANUS. Wait for **Hardware disconnected**; closing the browser tab alone does not disconnect the backend.

After disconnection:

- run history and artifact downloads remain available;
- static configuration validation remains available;
- acquisition, scans, and HV commands are rejected;
- a later connection performs discovery and configuration again.

# 5 Configuring an acquisition

## 5.1 Configuration source

The frontend opens with the checked-in production sample parsed into a categorized editor. The source remains a JANUS-format text document. Every editor change updates that document, and the exact resulting text is submitted to backend validation.

The line under **Acquisition parameters** reports the number of parsed settings. Comments and unknown/unexposed source lines remain in the text even when they are not rendered as controls.

The toolbar contains:

- **Reset sample** — replace all current edits with the bundled sample;
- **Save config** — save the current text to a local `.txt` file;
- **View raw configuration** / **Use parameter editor** — switch between graphical and source editing;
- **Import file** — replace the current document with a local `.txt`, `.cfg`, or plain-text file.

> **CAUTION — RESET AND IMPORT REPLACE EDITS**
>
> Save the current configuration before resetting or importing if it may be needed later.

**Save config** uses the browser file picker when available. In browsers without that API it asks for a filename and downloads the file. A `.txt` suffix is added if omitted.

## 5.2 Parameter categories and search

Category tabs correspond to JANUS sections such as **Connect**, **HV_bias**, **RunCtrl**, **AcqMode**, **Discr**, **Spectroscopy**, and **Test-Probe**.

Select **All** to show the cross-category **Find a parameter** search. It matches:

- parameter name;
- help/description;
- current value.

The count beside the search shows how many fields are visible. **No parameters match this filter** means the search returned no rendered fields; it does not mean the configuration is empty.

## 5.3 Control types

Parameters are rendered according to their definition:

- binary values are **Enabled/Disabled** switches;
- enumerated values are selection lists;
- bounded numeric values have a text/number entry, minus/plus buttons, range, and increment;
- free-form values use text inputs;
- `TempSensType` accepts a known sensor from suggestions or custom coefficients;
- masks open a 64-channel selector;
- board-scoped numeric values provide per-board overrides;
- channel-scoped numeric values provide per-channel overrides.

Native number fields also support typing and Arrow Up/Down. A value must meet its minimum, maximum, integer, and step requirements. Active invalid values are summarized above the raw editor and disable **Start run**.

The minus/plus buttons move by one defined increment and clamp at a defined minimum or maximum. Numeric override and mask dialogs can also be closed with **Cancel**, the Escape key, or a click on the backdrop; only their **Apply** button commits the dialog's current values.

Some fields are conditional:

- `ExtRunSource` is active for `StartRunMode TDL_EXTRUN`;
- `GPSTimeUTC` is active for `StartRunMode TDL_GPS`;
- `PresetTime` is active for `StopRunMode PRESET_TIME`;
- `PresetCounts` is active for `StopRunMode PRESET_COUNTS`;
- `ChTrg_Width` is active for `CountingMode PAIRED_AND`;
- `MajorityLevel` is active for majority trigger modes;
- test-pulse amplitude, destination, and preamplifier fields are active when the test-pulse source is not `OFF`.

## 5.4 Scope and inheritance

Configuration scope is one of:

- **Global** — one value applies to the system;
- **Board** — a general value can be overridden for board 0, 1, 2, or 3;
- **Channel** — a general value can be overridden for a specific board/channel pair.

An inherited value is not copied into a board/channel-specific assignment. The backend resolves it from the general value. An override creates or updates JANUS syntax:

```text
Parameter[board] value
Parameter[board][channel] value
```

The editor labels board summaries **inherited** or **override**.

## 5.5 Per-board overrides

For an eligible board-scoped field:

1. set the general value in the main row;
2. select **Per-board overrides**;
3. enter values for boards that must differ;
4. leave a field blank to inherit the general value;
5. select **Apply overrides**.

**Clear overrides** blanks all board-specific entries in the dialog. **Cancel** closes without applying the dialog state.

## 5.6 Per-channel overrides

Channel-scoped fields include timing/charge fine thresholds, HG/LG gain, zero-suppression thresholds, and individual HV adjustment.

1. set the general value;
2. select **Per-channel overrides**;
3. select board 0–3;
4. enter exceptions in the 64-channel grid;
5. leave a cell blank to inherit the general value;
6. select **Apply overrides**.

The main row summarizes the count of non-zero explicit values by board. **Clear board overrides** clears the displayed board's grid.

For `HV_IndivAdj`, the dialog also uses nominal board bias and the selected adjustment range to help interpret the per-channel value. The configuration stores the DAC value, not an independently commanded voltage.

## 5.7 Channel masks

The paired masks are:

- `ChEnableMask0/1`;
- `Tlogic_Mask0/1`;
- `Q_DiscrMask0/1`.

The low word covers channels 0–31 and the high word channels 32–63. Select **Configure channels** to open an 8×8 grid.

The target list contains **Global** and **Board 0** through **Board 3**. A board inherits the global pair until a board override is applied.

Mask actions are:

- **Enable all** — set all 64 bits;
- **Disable all** — clear all 64 bits;
- **Invert** — reverse every bit;
- a numbered channel — toggle that channel;
- **Apply mask** — write the two hexadecimal mask values;
- **Cancel** — close without applying.

Mask values are displayed as two 32-bit hexadecimal words. In stored run configuration details, a mask can be expanded to list the enabled channels in that word.

## 5.8 Temperature sensor input

`TempSensType` suggestions are:

- `TMP37`;
- `LM94021_G11`;
- `LM94021_G00`.

A custom sensor conversion may be entered as three coefficients:

```text
c0 c1 c2
```

The conversion is `T = V² × c2 + V × c1 + c0`. Confirm coefficient units and sensor wiring before using a custom conversion.

## 5.9 Raw configuration editor

Select **View raw configuration** to edit the exact source. This mode is intended for:

- reviewing comments and assignment order;
- adding syntax not conveniently represented by the graphical controls;
- pasting a complete known configuration;
- diagnosing a source-line validation issue.

Changes are parsed as you type. Select **Use parameter editor** to return to categorized controls.

The graphical editor deliberately excludes JANUS-only job control, desktop output-file switches, event-building settings not owned by this backend, and empty GUI sections. Section 13 lists the parameters exposed by this frontend.

## 5.10 Validation

Selecting **Start run** first calls backend static validation. Hardware is not mutated if validation fails.

Validation checks include:

- parse and assignment validity;
- supported production topology;
- scope and board/channel indices;
- known options and units;
- numeric ranges and increments;
- stop-policy requirements;
- acquisition/configuration compatibility;
- effective configuration constraints.

Issues are listed as:

```text
Line <number> · <field>
<message>
```

Warnings and errors are structurally distinct in the API, although the current list presents the returned message text. Backend validation is authoritative even when the browser has already accepted a field.

## 5.11 Run output settings

The following application settings appear at the end of the **RunCtrl** category. They are not JANUS hardware parameters.

### Store run histograms

When enabled, finalization writes `run_<run-id>.histograms.h5`. This artifact enables later viewing in the **Plots** workspace. It is enabled by default.

Disabling it saves storage and finalization work, but the completed run cannot later provide persisted histograms through the UI.

### Preserve complete raw batches

When enabled, the backend stores the complete received raw stream batches. Use it for protocol evidence, decoder investigation, or irreplaceable commissioning data. It increases storage use.

### Journal socket evidence

When enabled, the backend records socket traffic for transport diagnostics. It increases storage use and should be retained with fault evidence.

### HDF5 file size (MiB)

This is the event-segment rotation target. Valid values are integers from 1 through 1,048,576 MiB. The default is 500 MiB.

Segments are named:

```text
run_<run-id>.0000.h5
run_<run-id>.0001.h5
…
```

Rotation limits individual file size; it does not limit total run size.

### HDF5 compression

Choices are:

- **Blosc LZ4 · level 4 · bit-shuffle** — default production compression;
- **None** — uncompressed HDF5 datasets.

The setting applies to event segments and histogram artifacts. Compression changes storage representation, not decoded values.

# 6 Starting, monitoring, and stopping a run

## 6.1 Conditions for run start

**Start run** is enabled only when:

- the backend is not busy;
- system state is **Ready**;
- a non-empty configuration exists;
- every active browser-bounded numeric field is valid;
- a preset stop policy is valid;
- HDF5 segment size is an integer within range;
- no scan is active.

If the button is disabled, inspect system state, alerts, field errors, stop mode, and HDF5 size.

## 6.2 Stop policies

`StopRunMode` determines the configured policy:

- `MANUAL` — only operator stop or a failure ends the run;
- `PRESET_TIME` — automatic stop after `PresetTime`;
- `PRESET_COUNTS` — automatic stop after `PresetCounts` decoded events.

Preset time must be greater than zero. Preset count must be a positive integer.

The masthead summarizes the policy before start. During a preset run it shows the target and remaining time/events. **Stop and drain** remains available; automatic policy never removes manual stop.

An automatic stop uses the same serialized sequence as an operator stop: stop hardware, drain accepted data, close the pipeline, finalize artifacts, and publish **Ready**.

## 6.3 Starting

Before selecting **Start run**:

1. confirm **Ready** and four expected boards;
2. confirm intended HV state and stable Vmon/Imon;
3. review the exact configuration, particularly masks, thresholds, gain, test pulse, and stop policy;
4. verify storage health and free space outside the UI;
5. choose raw/journal/histogram evidence settings;
6. confirm the HDF5 segment and compression settings.

Then select **Start run** once.

The backend:

1. validates the submitted document;
2. compares it with the successfully applied configuration;
3. hard-applies and reads back changes when required;
4. synchronizes the DT5215/boards when session evidence requires it;
5. clears stale stream data;
6. allocates a run number and creates incomplete run evidence;
7. starts the pipeline and acquisition;
8. publishes **Running** with the exact active run identity.

Do not repeatedly select the button while configuration progress is active.

## 6.4 Active run display

The masthead shows:

- **Active run `<id>`**;
- decoded event count;
- stop policy;
- target and remaining time/events for preset modes.

When a scan is active, it instead shows **Scan in progress** and explains that acquisition controls are unavailable.

## 6.5 Pipeline panel

The **Acquisition** workspace pipeline panel displays:

| Field               | Meaning                                                                           |
| ------------------- | --------------------------------------------------------------------------------- |
| Phase               | **Acquiring**, **Draining buffered data**, **Finishing persistence**, or **Idle** |
| Received            | Events and transport batches received from the hardware                           |
| Raw persisted       | Raw batches written when raw capture is enabled                                   |
| Events persisted    | Stored decoded events compared with decoded events                                |
| Batch backlog       | Accepted batches not yet completed by the event-persistence stage                 |
| Ingress queue       | Current depth and bounded capacity                                                |
| Worker queues       | Raw and decoded-event worker queue depths                                         |
| Rejected / failures | Rejected batches / sum of decode and sink failures                                |

During a healthy steady run, queue depths may fluctuate but should not grow without recovery. Rejected batches or failures require investigation even if the run remains active.

## 6.6 Storage panel

The storage panel shows:

- storage health;
- run-directory path;
- bytes written in the current telemetry context;
- last storage error, if any.

“Healthy” confirms the backend's storage view; it is not a free-space meter. Monitor filesystem capacity separately.

## 6.7 Stop and drain

Select **Stop and drain** to end the exact active run.

The backend transitions through stop and drain states, stops reading new acquisition data, asks the hardware to stop, processes already accepted buffers, closes storage, computes artifact metadata, removes the incomplete marker only after successful finalization, and returns to **Ready**.

> **CAUTION — DO NOT TERMINATE DURING DRAIN**
>
> Wait until the state returns to **Ready** or a clear fault is reported. Killing the process during **Stopping** or **Draining** can leave a recoverable incomplete run.

“Stop” does not mean “discard queued data.” The explicit drain is why displayed event/artifact totals can continue changing briefly after the hardware stop command.

## 6.8 Completion reasons

Common persisted termination reasons include:

- operator stop;
- preset time reached;
- preset event count reached;
- completed scan;
- operator-cancelled scan;
- a specific failure message.

An **Incomplete** status means finalization did not complete authoritatively. Preserve the directory and evidence.

# 7 Statistics workspace

## 7.1 Live and stored sources

The upper-right source control shows:

- **Live source — Run `<id>`** while a run is active;
- **Live source — No active run** otherwise.

Selecting **Live source** returns from a historical run to live telemetry.

Expand **Stored data runs** at the bottom to select a completed data run with final statistics. Rows without final statistics are disabled. Search by exact non-negative run number or page through results.

A new active run automatically returns the Statistics workspace to live mode.

## 7.2 Global summary

For live data, the summary reports:

- decoded events;
- accepted batches;
- rejected batches;
- persisted bytes;
- elapsed run time.

For a historical source it reports:

- run identity;
- decoded event total;
- elapsed run time;
- that the values come from the finalized snapshot.

Legacy, incomplete, or unsuccessfully finalized runs may not contain final statistics.

## 7.3 All boards table

**All boards** compares:

| Column                  | Meaning                                                                         |
| ----------------------- | ------------------------------------------------------------------------------- |
| Board                   | Chain/board identity                                                            |
| Timestamp               | Latest DT5202 timestamp converted from 8 ns ticks to seconds                    |
| Trigger ID              | Latest event trigger ID                                                         |
| Received event rate     | Decoded non-service event rate                                                  |
| Estimated lost triggers | Forward gaps inferred from trigger IDs, as count and percentage                 |
| T-OR rate               | Rate derived from T-OR interval counters                                        |
| Decoded payload rate    | Decoded event payload bytes per second, excluding transport/descriptor overhead |

Live rates are differences between the two latest usable telemetry snapshots divided by the elapsed interval. Historical rates are averages over the completed run and show the total beside the rate.

Estimated lost triggers are diagnostic estimates based on forward trigger-ID gaps. They are not a direct hardware loss counter and must be interpreted with resets, wrap behavior, and acquisition mode in mind.

## 7.4 Board channel view

Select **Board 0** through **Board 3** for a 64-channel grid.

**Per-channel metric** choices are:

- **Channel trigger** — discriminator firings reported for each channel;
- **Timestamp** — channel events containing timing information;
- **PHA** — decoded pulse-height measurements used in energy spectra.

With **Cumulative counts** off:

- live values are rates over the latest telemetry interval;
- historical values are average rates over the complete run.

With **Cumulative counts** on, values are integrated counts since run start.

Changing the selected board, metric, or cumulative mode changes display calculations only. It does not reset counters, alter acquisition, or send hardware commands.

# 8 Plots workspace

## 8.1 Data source

The selected source is shown as:

- `Run <id> · live` for the active run;
- `Run <id>` for a persisted run;
- `None` when no usable source is selected.

Expand **Stored data runs** to select a completed data run containing an artifact whose kind is `histograms`. Runs without that artifact are disabled. When there is no active run, the first available stored run may be selected automatically.

## 8.2 Histogram types

The **Histogram** list contains:

- **PHA high gain**;
- **PHA low gain**;
- **Time of arrival**;
- **Time over threshold**.

Histograms are accumulated server-side. Selecting a type requests only the chosen board/channel datasets; large histogram arrays are not embedded in the telemetry stream.

Histogram availability and binning depend on acquisition mode and configuration. A valid request can return empty/zero-entry datasets when the selected event type was not produced.

## 8.3 Selecting channels

Select the **Channels** button to open a grid for each discovered board.

- choose individual channel numbers;
- **All** attempts to select the board's channels;
- **Clear** removes that board's channels;
- selected channels are highlighted.

At most 64 channels can be requested at once across all boards. Once the limit is reached, unselected channel buttons are disabled. Clear a board or individual channel before selecting a different one.

At least one channel is required. The initial selection is normally board 0, channel 0.

## 8.4 Refresh and axes

**Request data** performs an immediate request.

**Live refresh** requests the active run every second while:

- acquisition is running;
- the selected run is the active run;
- the switch is enabled.

It does not repeatedly refresh persisted runs. Changing run, histogram type, or selection also initiates a request. Obsolete responses are ignored if the source changes while a request is in flight.

**Log Y** switches the vertical count axis between linear and logarithmic display. Zero bins remain non-positive and cannot appear as logarithmic values.

Drag horizontally over the plot to zoom. Select **Reset zoom** to restore the full range. Cursor inspection and stepped overlays allow comparison of selected channels.

## 8.5 Dataset metadata

Below the plot, each selected dataset reports:

- board and channel;
- histogram kind;
- number of bins;
- entries;
- bin width;
- populated-bin count;
- peak bin count;
- underflow and overflow, when non-zero.

Underflow means values below the histogram minimum. Overflow means values at or above its represented range. These entries are counted but not placed in ordinary visible bins.

# 9 Scans workspace

Both scan cards are collapsed by default. Select **Expand** to show controls and stored datasets; select **Collapse** to hide them without cancelling an active scan.

Scans:

- require **Ready**;
- are exclusive with data runs, configuration, and other scans;
- receive normal numeric run IDs;
- alter diagnostic registers temporarily;
- attempt to restore the prior configuration afterward;
- publish preparing, running, restoring, completed, cancelled, or failed state.

Do not assume restoration succeeded after a failed scan. Inspect final status, system state, and diagnostics.

## 9.1 Threshold staircase

### Purpose

A threshold staircase measures discriminator activity versus coarse threshold. The system changes both TD and QD coarse thresholds and records:

- 64 individual timing-discriminator channel counts/rates;
- T-OR count/rate;
- Q-OR count/rate.

The scan runs from the configured maximum threshold downward to the minimum.

### Controls

| Control    |    Allowed value | Meaning                            |
| ---------- | ---------------: | ---------------------------------- |
| Board      |              0–3 | Board/chain to scan                |
| Minimum    |           0–2047 | Inclusive lowest coarse threshold  |
| Maximum    |           0–2047 | Inclusive highest coarse threshold |
| Step       | positive integer | Threshold decrement between points |
| Dwell (ms) |         1–34,000 | Counting interval per point        |

Minimum must not exceed maximum. The scan may contain no more than 4,096 points.

Total point count is:

```text
floor((maximum - minimum) / step) + 1
```

A point is measured at the maximum, then at descending step values. If the range is not exactly divisible by the step, the final measured value is the last step value not below the minimum.

Longer dwell produces steadier low-rate estimates but increases scan duration. Each point also includes hardware setup/readout overhead.

For a dark-count staircase, turn off the light source and confirm the intended HV state before selecting **Start scan**.

### Running and cancelling

Select **Start scan**. The status shows run number, completed/total points, scan state, and progress.

Select **Cancel** to request cancellation. Cancellation is not complete until restoration finishes and the system leaves **Scanning**.

### Plot

**Curve** choices are:

- Channel 0 TD through Channel 63 TD;
- T-OR;
- Q-OR.

**Log Y** is useful for comparing rates across orders of magnitude. Drag horizontally to zoom and use **Reset zoom** to restore the threshold range.

### Finalized staircases

The stored list shows run, start time, board, points, and status. Filter by **Any board** or a specific board, select **Refresh**, and use **Previous/Next** for pages of eight.

Select a row to load and plot that scan. A nonnumeric legacy scan ID is labeled **Legacy scan**.

## 9.2 Hold-delay scan

### Purpose

A hold-delay scan varies the delay between trigger/peak-detection start and hold, collects a requested number of spectroscopy events at each point, and builds one 512-bin high-gain spectrum for every channel.

The heatmap allows the operator to locate the delay region where high-gain pulse height is maximized or otherwise has the desired shape.

### Controls

| Control        |                      Allowed value | Meaning                                           |
| -------------- | ---------------------------------: | ------------------------------------------------- |
| Board          |                                0–3 | Board/chain to scan                               |
| Minimum (ns)   |         non-negative multiple of 8 | Inclusive first requested delay                   |
| Maximum (ns)   |   multiple of 8, not below minimum | Inclusive upper delay                             |
| Step (ns)      | positive multiple of 8, at least 8 | Increment between delay points                    |
| Events / delay |                         10–100,000 | Accepted spectroscopy events to collect per point |
| Timeout (s)    |                              1–600 | Maximum collection time at one point              |

The scan must contain from 1 through 64 delay points. The UI defaults match the JANUS scan convention: 0–256 ns, 8 ns step, 100 events, and 30 s timeout.

At each point the backend:

1. clears stale stream data;
2. writes delay in 8 ns hardware units;
3. reads the value back;
4. starts acquisition on the selected board;
5. collects the requested event count or reaches the point timeout;
6. stops acquisition;
7. stores channel spectra and missing-channel counts.

The dataset records both requested and effective delay. A readback mismatch fails the scan.

### Running, cancelling, and plot

Select **Start scan**. Progress reports completed and total delay points.

Select **Cancel** to request cancellation, then wait for restoration.

Choose **Channel 0** through **Channel 63**. The heatmap horizontal axis is hold delay; the vertical axis is high-gain energy bin. Color intensity is logarithmic event count, with the color scale displayed beside the plot.

Drag over the heatmap to zoom horizontally and vertically. Unlike the histogram and staircase plots, the heatmap currently has no **Reset zoom** button; selecting another channel or reloading/re-rendering the dataset rebuilds its view.

### Finalized hold-delay scans

The list shows the latest 12 hold-delay scans with run, start time, board, points, and status. Select **Refresh** to reload it. Select a row to load its spectra.

# 10 Hardware and high-voltage workspace

## 10.1 Board cards

One card is shown for each discovered board. It contains:

- chain and node;
- product identity `DT5202`;
- health: Unknown, OK, Degraded, or Fault;
- HV state;
- FPGA firmware in hexadecimal;
- board temperature;
- detector temperature;
- FPGA temperature;
- HV-module temperature;
- Vmon in volts;
- Imon in milliamperes;
- board event count;
- last telemetry update time;
- board-level HV button.

**Waiting for discovered boards…** appears when no boards are in the current snapshot.

The frontend computes HV display state in this priority:

1. over-current or over-voltage → **HV fault**;
2. ramping → **Ramping**;
3. on → **HV on**;
4. otherwise → **HV off**.

The masthead **SiPM bias** indicator summarizes these per-board states.

## 10.2 High-voltage measurements

- **Vmon** is measured output voltage, not the configured setpoint.
- **Imon** is measured current; the UI converts the backend value from amperes to milliamperes.
- **HV temp.** is the HV module temperature.
- **Detector temp.** is the configured detector sensor conversion.
- **Telemetry updated** is the wall-clock observation time for service telemetry.

If HV telemetry reads fail, board health can become **Degraded**. Over-current or over-voltage makes it **Fault**.

## 10.3 Authorization and state gates

HV buttons are enabled only while the system is **Ready** and the backend is not busy.

HV-on is rejected unless the backend has `-authorize-hv-config`. HV-off does not require this authorization but still requires **Ready** through the normal UI/API path.

The authorization flag also controls whether configured HV peripheral setpoints may be applied during hardware configuration. Starting without it is the conservative mode for readout commissioning that must not change setpoints.

## 10.4 All-board control

**All HV on** requests HV enable for every configured board. If enabling one target fails, the backend attempts to turn back off targets it enabled earlier in the same operation.

**All HV off** requests disable for every board. Review each card afterward; a command failure or stale telemetry can mean not every board reached the requested state.

## 10.5 Board-level control

Each card contains:

- **Turn board `<n>` HV on** when `hvOn` is false;
- **Turn board `<n>` HV off** when `hvOn` is true.

During ramping, use Vmon, Imon, and the ramping indication rather than repeatedly toggling the command.

## 10.6 Recommended HV-on sequence

1. confirm **Ready** and all board health;
2. verify `HV_Vbias`, `HV_Imax`, individual adjustment, temperature sensor, and feedback configuration;
3. confirm detector cooling and site interlocks;
4. prefer enabling one board during commissioning;
5. watch **Ramping**, Vmon, Imon, and temperatures;
6. confirm stable **HV on** without fault;
7. repeat for remaining boards or use **All HV on** only under an approved procedure.

If a fault is shown, stop acquisition if active, preserve diagnostics, and follow the site's HV shutdown/recovery procedure.

# 11 Run history, search, and downloads

## 11.1 Run table

The **Runs** workspace loads 20 newest records at a time, ordered by start time and run ID. Select **Refresh** to reload the first page and **Load more** to append older records.

Columns are:

- run number;
- type;
- start date;
- duration;
- event count (data runs only);
- total recorded artifact size;
- termination/status.

Select a row or its run-number link to expand details. Select it again or **Close** to collapse it.

## 11.2 Run details

The detail panel shows:

- type;
- start and completion time;
- duration;
- termination;
- events and raw batches for data runs;
- aggregate artifact size;
- stop mode and preset target, when recorded.

An absent completion time is shown as **In progress** for incomplete records or **Not reported** otherwise.

## 11.3 Artifacts

Every artifact button shows:

- filename;
- artifact kind;
- size;
- **Download**.

Selecting it downloads the exact regular file recorded in the finalized manifest. The backend will not serve an arbitrary path supplied by the browser.

The manifest records SHA-256 metadata even though the collapsed button does not display the hash. Preserve the manifest with downloaded scientific/evidence files when provenance matters.

## 11.4 Recorded configuration

For a data run, expanding the row requests the exact submitted JANUS configuration. It is grouped by section into tables containing:

- parameter;
- scope;
- value;
- description.

Select a section summary to expand it. The first section opens initially.

For masks, select the hexadecimal value to show channels enabled in that 32-bit word.

Select **Download configuration (.txt)** to download:

```text
run_<run-id>_configuration.txt
```

Scan records do not have this configuration table. Their detail directs the operator to the **Scans** workspace for the stored dataset.

## 11.5 Searching stored runs

Select **Search configurations** to open the search form.

All specified filters are combined with logical AND: every filter must match.

### Configuration filters

Select **Add filter** for additional predicates. For each predicate:

1. search and choose a parameter from the list;
2. optionally choose board and channel scope;
3. choose exact or range matching for numeric values;
4. enter the value/minimum and optional maximum.

Parameter suggestions match name, section, and description. The parameter must be selected from the catalog; arbitrary names are rejected.

Scope behavior:

- global parameters search the requested global assignment;
- board/channel parameters search resolved effective values;
- **All boards** means any matching board;
- **All channels** means any matching channel on the selected/any board;
- a channel cannot be selected until a board is selected.

For numeric ranges, either bound may be blank, but not both. Minimum must not exceed maximum. Text/choice parameters use exact equality.

Values use canonical catalog units; use the range hint displayed beside the input.

Select **Remove** to remove a predicate. At least one blank predicate row remains in the form.

### Run metadata filters

Available metadata filters are:

- **Run type** — all, data runs, staircase scans, or hold-delay scans;
- **Run number** — exact number, or inclusive lower bound if maximum is supplied;
- **Maximum run number** — optional inclusive upper bound; requires run number;
- **Minimum events** — data-run event-count lower bound.

Run numbers and minimum events must be non-negative integers.

### Results

Select **Search runs**. Search returns 20 records at a time. **Load more** appends another page. **No runs match these filters** means the query succeeded with no match.

Select **Clear** to reset filters and return to the ordinary run-history table.

## 11.6 Stored-run pickers in Statistics and Plots

The compact **Stored data runs** pickers in **Statistics** and **Plots** are intentionally simpler than the Runs search:

- they include data runs only;
- they search exact run number only;
- they show eight records per page;
- unavailable rows are disabled;
- **Previous** and **Next** navigate pages;
- **Clear** removes the exact-number search.

# 12 System states, diagnostics, and recovery

## 12.1 State reference

| State            | Meaning and permitted operator action                                                                 |
| ---------------- | ----------------------------------------------------------------------------------------------------- |
| **Disconnected** | Backend/API is running without a DT5215 session. Connect hardware; history remains usable.            |
| **Connecting**   | Opening transports and discovering hardware. Wait; do not start another client.                       |
| **Idle**         | Hardware session exists but is not yet fully ready/configured.                                        |
| **Configuring**  | Applying and verifying configuration. Watch progress and wait.                                        |
| **Ready**        | Safe command state for run start, scan start, and HV switching.                                       |
| **Starting**     | Synchronizing, clearing stream, creating storage, and starting acquisition.                           |
| **Running**      | A data run is active. Stop/drain is available.                                                        |
| **Stopping**     | Hardware stop is in progress. Wait.                                                                   |
| **Draining**     | Accepted buffered data is being persisted/finalized. Wait.                                            |
| **Recovering**   | The backend is attempting bounded recovery. Preserve diagnostics.                                     |
| **Scanning**     | One diagnostic scan is active/restoring. Run controls are unavailable.                                |
| **Fault**        | An operation failed or safe state could not be proven. Record evidence and follow recovery procedure. |

## 12.2 Browser staleness

If the browser says **Backend offline** while the process is expected to be running:

1. do not infer that hardware stopped;
2. check network/browser/backend logs;
3. select **Retry backend**;
4. wait for a fresh authoritative snapshot;
5. never start a second backend merely to restore the UI.

A lost browser connection does not itself cancel a run. Run control is owned by the backend.

## 12.3 Run fault

If a run faults:

1. record the system diagnostic, active run ID, state, board/HV status, and time;
2. stop other clients from sending hardware commands;
3. do not remove the run directory or incomplete marker;
4. preserve `manifest.json`, HDF5/JSON event files, `wire.raw`, and `transport.journal` if present;
5. understand the primary error before restarting;
6. after hardware/network faults, repeat read-only acceptance.

Startup inspects incomplete runs and reports them; it does not silently delete evidence.

## 12.4 Hardware left running

On startup the backend detects boards left in an acquisition state and attempts a bounded stop, drain, global reset, and ready-state verification. A failed recovery remains visible as a fault/diagnostic.

Do not power-cycle or reconnect hardware merely to clear the screen. Follow the approved site recovery plan and retain the original error.

## 12.5 Catalog problems

Run manifests are authoritative; the SQLite catalog is a search index.

With the backend stopped or according to the documented maintenance procedure:

```sh
task catalog:check
```

To rebuild from manifests:

```sh
task catalog:rebuild
```

To back up the catalog to a new file:

```sh
task catalog:backup DEST=path/to/backup.sqlite3
```

Do not rebuild solely to hide a corrupt or unexplained manifest. Preserve and investigate the evidence first.

# 13 Configuration parameter reference

This section documents every JANUS parameter intentionally exposed by the frontend catalog. “Scope” describes how the graphical editor applies inheritance.

## 13.1 Connect

| Parameter | Scope | Description                                                                                                                                                                                                                                            |
| --------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Open`    | Board | JANUS connection descriptor retained in the configuration document. The production backend command-line `-control` and `-stream` addresses select the DT5215 transports; the document is still validated for the required four board/link assignments. |

## 13.2 HV_bias

| Parameter            | Scope           | Values / range             | Description                                                                                              |
| -------------------- | --------------- | -------------------------- | -------------------------------------------------------------------------------------------------------- |
| `HV_Vbias`           | Board           | 20–85 V, step 0.1 V        | Common nominal detector bias for a board. Fine channel adjustment is provided by `HV_IndivAdj`.          |
| `HV_Imax`            | Board           | non-negative; step 0.1 mA  | Maximum HV output current. The HV module shuts down on over-current.                                     |
| `HV_Adjust_Range`    | Global          | `4.5`, `2.5`, `DISABLED`   | Voltage span of the individual per-channel adjustment DAC.                                               |
| `HV_IndivAdj`        | Channel         | 0–255 integer              | Individual channel HV-adjust DAC code. The effect depends on nominal bias and adjustment range.          |
| `Vnom`               | Channel monitor | derived                    | Estimated per-channel bias. It is a monitor concept and is not saved as an ordinary editable assignment. |
| `TempSensType`       | Global          | named sensor or `c0 c1 c2` | Detector temperature sensor/conversion.                                                                  |
| `TempFeedbackCoeff`  | Global          | step 0.1 mV/°C             | Compensation coefficient in `Vout = Vset + k × (T − 25°C)`.                                              |
| `EnableTempFeedback` | Global          | 0/1                        | Enables bias temperature feedback.                                                                       |

## 13.3 RunCtrl

| Parameter            | Scope  | Values / range                                                  | Description                                                                                                                 |
| -------------------- | ------ | --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `StartRunMode`       | Global | `ASYNC`, `TDL`, `TDL_EXTRUN`, `TDL_GPS`, `CHAIN_T0`, `CHAIN_T1` | Hardware start coordination mode. Confirm synchronization wiring before using an external mode.                             |
| `ExtRunSource`       | Global | `SYNC-IN`, `LEMO_RA`, `LEMO_RB`, `LEMO_FA`, `LEMO_FB`           | DT5215 external run-start source; active with `TDL_EXTRUN`.                                                                 |
| `GPSTimeUTC`         | Global | `YYYY-MM-DD HH:MM:SS`                                           | UTC target used with `TDL_GPS`.                                                                                             |
| `StopRunMode`        | Global | `MANUAL`, `PRESET_TIME`, `PRESET_COUNTS`                        | Manual or automatic stop policy enforced by the backend.                                                                    |
| `PresetTime`         | Global | positive when active; `s`, `ms`, `us`, or `ns`                  | Automatic duration for `PRESET_TIME`.                                                                                       |
| `PresetCounts`       | Global | positive integer when active                                    | Automatic decoded-event target for `PRESET_COUNTS`.                                                                         |
| `RunNumber_AutoIncr` | Global | 0/1                                                             | JANUS compatibility setting retained in the editor. Server run IDs always come from the shared monotonic catalog allocator. |

The frontend also adds the five application output settings described in Section 5.11.

## 13.4 AcqMode

| Parameter             | Scope  | Values / range                                                                          | Description                                                                                                                                                                |
| --------------------- | ------ | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `AcquisitionMode`     | Global | `SPECTROSCOPY`, `SPECT_TIMING`, `TIMING_CSTART`, `TIMING_CSTOP`, `COUNTING`, `WAVEFORM` | Selects the DT5202 event/acquisition mode. It determines which decoded fields and histograms can receive data.                                                             |
| `EnableToT`           | Global | 0/1                                                                                     | In timing mode, enables 9-bit ToT with 16-bit ToA. Disabled uses 25-bit ToA without ToT.                                                                                   |
| `EnableListZeroSuppr` | Global | 0/1                                                                                     | Suppresses timing events with zero hits in list output semantics.                                                                                                          |
| `BunchTrgSource`      | Global | `T0-IN`, `T1-IN`, `Q-OR`, `T-OR`, `TLOGIC`, `PTRG`                                      | Trigger source for spectroscopy and counting modes.                                                                                                                        |
| `VetoSource`          | Global | `DISABLED`, `SW_CMD`, `T0-IN`, `T1-IN`                                                  | Active-high source that inhibits the bunch trigger.                                                                                                                        |
| `ValidationSource`    | Global | `SW_CMD`, `T0-IN`, `T1-IN`                                                              | Trigger-validation source expected in the validation window.                                                                                                               |
| `ValidationMode`      | Global | `DISABLED`, `ACCEPT`, `REJECT`                                                          | Accepts only validated triggers, rejects triggers with validation, or disables validation.                                                                                 |
| `CountingMode`        | Global | `SINGLES`, `PAIRED_AND`                                                                 | Independent channel counting or pair coincidence counting.                                                                                                                 |
| `ChTrg_Width`         | Global | 0 or 8–2032 ns, step 8 ns                                                               | Coincidence window for `PAIRED_AND`; 0 disables it.                                                                                                                        |
| `EnableCntZeroSuppr`  | Global | 0/1                                                                                     | Suppresses zero-count channels in counting events.                                                                                                                         |
| `TrgIdMode`           | Global | `TRIGGER_CNT`, `VALIDATION_CNT`                                                         | Trigger ID counts all triggers or validation signals.                                                                                                                      |
| `TriggerLogic`        | Global | `OR64`, `AND2_OR32`, `OR32_AND2`, `OR16_AND4`, `MAJ64`, `MAJ32_AND2`                    | Combinatorial network over 64 self-trigger inputs.                                                                                                                         |
| `Tlogic_Width`        | Global | non-negative, 8 ns increments                                                           | Trigger-logic output width; 0 selects linear behavior.                                                                                                                     |
| `MajorityLevel`       | Global | 1–64 integer                                                                            | Required multiplicity for majority trigger modes.                                                                                                                          |
| `PtrgPeriod`          | Global | non-negative, 8 ns increments                                                           | Internal periodic-trigger period.                                                                                                                                          |
| `TrefSource`          | Global | `T0-IN`, `T1-IN`, `Q-OR`, `T-OR`, `PTRG`, `TLOGIC`                                      | Timing common-start/common-stop reference.                                                                                                                                 |
| `TrefWindow`          | Global | non-negative, 8 ns increments                                                           | Timing reference gate/window.                                                                                                                                              |
| `TrefDelay`           | Global | −4,194,304 to 4,194,296 ns                                                              | Signed timing-reference delay. Hardware sampling is in 8 ns units even where legacy definition metadata permits unit-step input.                                           |
| `T0_Out`              | Global | listed signal choices                                                                   | Signal routed to T0 output. Choices include input, bunch trigger, T-OR, trigger logic, run, periodic trigger, busy, digital probe, square wave, synchronization, and zero. |
| `T1_Out`              | Global | listed signal choices                                                                   | Signal routed to T1 output. Similar to T0, with Q-OR and T1 input choices.                                                                                                 |
| `ChEnableMask0/1`     | Board  | 64-bit mask pair                                                                        | Enables acquisition channels 0–63.                                                                                                                                         |

## 13.5 Discr

| Parameter            | Scope   | Values / range                | Description                                                     |
| -------------------- | ------- | ----------------------------- | --------------------------------------------------------------- |
| `FastShaperInput`    | Global  | `HG-PA`, `LG-PA`              | Connects the fast shaper to the high- or low-gain preamplifier. |
| `TD_CoarseThreshold` | Board   | 0–2047 integer                | Common timing discriminator coarse threshold for the board.     |
| `TD_FineThreshold`   | Channel | 0–15 integer                  | Individual channel timing discriminator trim.                   |
| `Hit_HoldOff`        | Global  | non-negative, 8 ns increments | Trigger hold-off/dead time.                                     |
| `Tlogic_Mask0/1`     | Board   | 64-bit mask pair              | Selects channels that feed trigger logic.                       |
| `QD_CoarseThreshold` | Global  | 0–2047 integer                | Common charge discriminator coarse threshold.                   |
| `QD_FineThreshold`   | Channel | 0–15 integer                  | Individual channel charge discriminator trim.                   |
| `Q_DiscrMask0/1`     | Board   | 64-bit mask pair              | Selects channels contributing to Q-OR.                          |

## 13.6 Spectroscopy

| Parameter         | Scope   | Values / range                                          | Description                                                                                                                             |
| ----------------- | ------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `GainSelect`      | Global  | `HIGH`, `LOW`, `AUTO`, `BOTH`                           | Chooses which gain measurement is represented: HG, LG, automatic HG-unless-saturated, or both.                                          |
| `HG_Gain`         | Channel | 1–63 integer                                            | High-gain preamplifier setting.                                                                                                         |
| `LG_Gain`         | Channel | 1–63 integer                                            | Low-gain preamplifier setting.                                                                                                          |
| `Pedestal`        | Global  | 0–16383 integer                                         | Common desired ADC pedestal. Hardware programming incorporates protected-flash calibration.                                             |
| `ZS_Threshold_LG` | Channel | 0–65535 integer                                         | Low-gain zero-suppression threshold.                                                                                                    |
| `ZS_Threshold_HG` | Channel | 0–65535 integer                                         | High-gain zero-suppression threshold.                                                                                                   |
| `HG_ShapingTime`  | Global  | 12.5, 25, 37.5, 50, 62.5, 75, 87.5 ns                   | High-gain slow-shaper time.                                                                                                             |
| `LG_ShapingTime`  | Global  | 12.5, 25, 37.5, 50, 62.5, 75, 87.5 ns                   | Low-gain slow-shaper time.                                                                                                              |
| `HoldDelay`       | Global  | non-negative                                            | Delay from bunch trigger/start of peak detection to hold/stop.                                                                          |
| `MuxClkPeriod`    | Global  | non-negative                                            | Multiplexer readout period; the definition recommends 300 ns.                                                                           |
| `EHistoNbin`      | Global  | `DISABLED`, `256`, `512`, `1K`, `2K`, `4K`, `8K`        | Server live PHA histogram binning. Rebins the plot only; it does not change hardware, decoded energies, or stored event values.         |
| `ToAHistoNbin`    | Global  | `DISABLED`, `256`, `512`, `1K`, `2K`, `4K`, `8K`, `16K` | ToA histogram bin count.                                                                                                                |
| `ToARebin`        | Global  | 1–127 integer                                           | ToA rebin factor; bin width is 0.5 ns times this value.                                                                                 |
| `ToAHistoMin`     | Global  | time value                                              | Minimum represented ToA.                                                                                                                |
| `MCSHistoNbin`    | Global  | `DISABLED`, `256`, `512`, `1K`, `2K`, `4K`, `8K`, `16K` | Multi-channel scaler histogram size for counting mode. The current Plots workspace exposes PHA, ToA, and ToT, not an MCS plot selector. |

## 13.7 Test-Probe

| Parameter              | Scope  | Values / range                                                                                                           | Description                                                               |
| ---------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- |
| `AnalogProbe0`         | Global | `OFF`, `FAST`, `SLOW_LG`, `SLOW_HG`, `PREAMP_LG`, `PREAMP_HG`                                                            | Signal routed to analog probe 0.                                          |
| `DigitalProbe0`        | Global | `OFF`, `PEAK_LG`, `PEAK_HG`, `HOLD`, `START_CONV`, `DATA_COMMIT`, `DATA_VALID`, `CLK_1024`, `VAL_WINDOW`, `T_OR`, `Q_OR` | Signal routed to digital probe 0.                                         |
| `ProbeChannel0`        | Global | 0–31 integer                                                                                                             | Channel observed by probe group 0.                                        |
| `AnalogProbe1`         | Global | same analog choices                                                                                                      | Signal routed to analog probe 1.                                          |
| `DigitalProbe1`        | Global | same digital choices                                                                                                     | Signal routed to digital probe 1.                                         |
| `ProbeChannel1`        | Global | 32–63 integer                                                                                                            | Channel observed by probe group 1.                                        |
| `TestPulseSource`      | Global | `OFF`, `EXT`, `T0-IN`, `T1-IN`, `PTRG`, `SW-CMD`                                                                         | Source that generates/injects the test-pulse action.                      |
| `TestPulseAmplitude`   | Global | 0–4095 integer                                                                                                           | 12-bit internal test-pulser DAC setting. Active when source is not `OFF`. |
| `TestPulseDestination` | Global | `NONE`, `ALL`, `EVEN`, `ODD`, or `CH 0`…`CH 63`                                                                          | Channels connected to the test pulse.                                     |
| `TestPulsePreamp`      | Global | `LG`, `HG`, `BOTH`                                                                                                       | Preamplifier path receiving the test pulse.                               |

## 13.8 Parameters intentionally not exposed as normal controls

The frontend excludes settings owned by other parts of this application or by the JANUS desktop workflow:

- JANUS job range/sleep controls;
- JANUS destination-directory and list-file controls;
- JANUS raw/list/CSV/service/sync/run-info output switches;
- JANUS histogram-output enable switches;
- JANUS event-building/sorting settings;
- JANUS data-analysis switch;
- empty `Regs`, `Statistics`, and `Log` desktop tabs.

Equivalent application functions are provided by run output settings, server storage, the Statistics/Plots workspaces, run history, and artifact downloads. Do not add excluded assignments expecting the frontend to make them active application features.

# 14 Storage and artifact reference

## 14.1 Run directory

Each run has durable metadata and one or more data artifacts under the configured runs parent. Production event output is segmented HDF5. Development builds without the HDF5 tag can use JSON Lines storage.

The manifest records:

- run identity and timestamps;
- requested configuration;
- stop/termination details;
- event/raw-batch counts;
- incomplete/finalization state;
- artifact kind, name, size, and SHA-256;
- final statistics when successfully available.

## 14.2 Common artifact kinds

Depending on build and selected output settings, a data run can include:

- HDF5 event segments;
- JSON Lines decoded events in development;
- `run_<id>.histograms.h5`;
- byte-exact raw batch capture;
- transport journal;
- manifest/configuration evidence.

Scan runs store their own finalized staircase or hold-delay dataset artifact and manifest.

## 14.3 Evidence retention

For an acceptance or diagnostic run, retain together:

- all event segments;
- histogram artifact;
- raw capture and transport journal when enabled;
- manifest;
- exact configuration;
- backend log;
- relevant packet capture;
- DT5215 and DT5202 firmware identities;
- operator, date, topology, and test procedure.

Verify artifact size and SHA-256 against the manifest after copying.

# 15 Recommended operating procedures

## 15.1 Pre-run checklist

1. Confirm JANUS is disconnected and closed and no other DT5215/DT5202 client is running.
2. Confirm correct physical topology and DT5215 link provisioning.
3. Confirm backend and hardware are online.
4. Confirm system state is **Ready**.
5. Confirm four board cards and acceptable health/temperatures.
6. Confirm intended HV state, stable Vmon/Imon, and no HV fault.
7. Load or edit the exact approved configuration.
8. Review all board/channel overrides and masks.
9. Review acquisition mode, trigger source, thresholds, gains, hold delay, and test pulse.
10. Review manual/preset stop policy.
11. Review histogram/raw/journal evidence settings.
12. Confirm HDF5 segment size/compression and filesystem free space.
13. Save a local copy of the configuration.
14. Select **Start run** once and observe configuration/start progress.

## 15.2 During-run checklist

1. Confirm state remains **Running** and active run ID is recorded.
2. Watch event count and board statistics.
3. Watch rejected batches, decode failures, sink failures, queue depths, and backlog.
4. Watch storage health and bytes written.
5. Watch HV, current, and temperatures.
6. Use plots/statistics display controls freely; they do not change acquisition.
7. Record any diagnostic immediately.

## 15.3 End-run checklist

1. Select **Stop and drain**, or observe the configured automatic stop.
2. Wait for **Ready**.
3. Open **Runs**, select **Refresh**, and find the run ID.
4. Confirm completion reason, duration, event count, size, and no incomplete status.
5. Confirm expected artifacts exist.
6. Download or archive configuration/evidence as required.
7. If shutting down detector bias, issue HV-off while **Ready** and confirm Vmon/state.
8. Select **Disconnect hardware** in the upper-right operations area and wait for **Hardware disconnected**.
9. Only after hardware disconnection, close the browser or stop the Docker container.

## 15.4 Scan checklist

1. Ensure no data run is active and state is **Ready**.
2. Confirm intended illumination and HV conditions.
3. Expand the correct scan card.
4. Verify board and bounds.
5. Estimate points and duration.
6. Start the scan and watch progress.
7. If cancelling, wait through restoration.
8. Confirm final status and return to **Ready**.
9. Load the finalized dataset and inspect it.
10. Reconfirm acquisition/HV configuration before the next physics run.

# 16 Troubleshooting

## Start run is disabled

Check, in order:

1. backend must be online and fresh;
2. hardware must be connected;
3. system state must be **Ready**;
4. no scan may be active;
5. no configuration value may be out of range or off-step;
6. `PRESET_TIME` requires positive time;
7. `PRESET_COUNTS` requires a positive integer;
8. HDF5 size must be an integer from 1 to 1,048,576;
9. another command must not be busy.

## Start run produces validation issues

Use the reported field and source line. Switch to **View raw configuration**, inspect that line and any later override of the same parameter, correct it, then start again. Backend validation can reject combinations not detectable from one field in isolation.

## Hardware will not connect

Stop all other clients. Verify control/stream addresses, routing, DT5215 power, four enabled links, one node-0 board per link, and firmware state. Run read-only acceptance. Do not automate or write persistent link enablement.

## Backend is offline but acquisition may still be running

Do not start another backend. Restore browser/backend connectivity, use **Retry backend**, and wait for the authoritative snapshot. If the backend process actually failed, follow fault-evidence and recovery procedures.

## HV-on is rejected

The system must be **Ready**, no command may be busy, and the backend must have `-authorize-hv-config`. Do not restart with authorization until the configured setpoints and safety procedure have been reviewed.

## HV says ramping for too long

Observe Vmon, Imon, temperatures, diagnostics, and module status. Do not repeatedly toggle HV. Follow the site procedure for a stalled ramp or excessive current.

## No statistics appear

Live statistics require an active run and received events. Historical statistics require a successfully finalized run containing `finalStatistics`; legacy and incomplete runs may not have them.

## A historical run is disabled in the Statistics picker

It has no final statistics snapshot. Inspect it in **Runs** for incomplete status, legacy format, or failure.

## No histogram is displayed

Confirm:

- a run is selected;
- at least one channel is selected;
- the histogram type is produced by the acquisition mode;
- for a completed run, a `histograms` artifact exists;
- **Store run histograms** was enabled before that run;
- the request did not report a backend/storage error.

## Live histogram stopped refreshing

Live refresh only runs while the selected run is the active run and system state is **Running**. Check **Live refresh**, selected run, backend freshness, and run state. Use **Request data** for an immediate request.

## Cannot select more histogram channels

The request limit is 64 total channels across all boards. Clear channels or a selected board first.

## Staircase start fails

Confirm **Ready**, board 0–3, thresholds 0–2047, positive step, minimum not above maximum, dwell 1–34,000 ms, and no more than 4,096 points.

## Hold-delay start fails

Confirm **Ready**, board 0–3, delay bounds and step are multiples of 8 ns, step is at least 8 ns, event target is 10–100,000, timeout is 1–600 s, and point count is 1–64.

## Scan was cancelled but controls remain unavailable

Cancellation is followed by restoration. Wait until state leaves **Scanning**. If it enters **Fault**, preserve the diagnostic and do not assume the previous configuration was restored.

## Run history is empty

Select **Refresh**. Confirm the configured runs directory, catalog availability, permissions, and that manifests exist. An active run may not appear as finalized history until stop/drain completes.

## Search returns no expected run

Remember that all filters must match. Check:

- requested versus resolved scope;
- board/channel selection;
- exact versus range;
- canonical units;
- run type;
- inclusive run-number bounds;
- minimum event count.

Clear the search and locate the run in ordinary history to inspect its recorded configuration.

## Artifact download fails

The backend serves only regular artifact files named in the run manifest. Confirm the file still exists, matches the run, and the backend can read it. Do not edit the manifest to bypass the restriction.

## Storage reports an error or queues keep growing

Stop and drain if it remains safely available. Record counters and the error. Check capacity, permissions, filesystem health, and storage latency. Preserve incomplete evidence if drain/finalization fails.

---

## Reference documents

- `STMP_UM7946_Janus_UserManual_rev3.pdf` — organizational/style reference and JANUS terminology
- `WEB_UM7945_A5202-DT5202_rev4.pdf` — DT5202 hardware reference
- `UM8977_DT5215_UserManual_rev2.pdf` — DT5215 hardware reference
- `CITIROC1A - Datasheet V2.53.pdf` — CITIROC ASIC reference
- `docs/hardware-operations.md` — project hardware provisioning, acceptance, and recovery procedures
- `docs/architecture.md` — implemented software architecture
- `docs/daq_protocol_notes.md` — protocol evidence classification and wire behavior
