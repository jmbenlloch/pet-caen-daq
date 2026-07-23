# Running JANUS against the simulator

JANUS 5.0.0/FERSlib 2.2.0 can use the simulator through the same DT5215
Ethernet protocol as real hardware. Both use TCP port 9760 for control and 9000
for streaming.

## Build JANUS

Install a C++17 compiler, Make, pkg-config, and the libusb 1.0 development
package. Gnuplot with the `wxt` terminal is required when JANUS online analysis
is enabled. The Python GUI additionally requires Python 3, Tkinter, and Pillow.

From `janus/Janus_5202_5.0.0_20260713_linux`:

```sh
./installFERSlibJanus.bash
```

This installs the bundled FERSlib under `ferslib/local`, generates `Makefile`,
and builds `bin/JanusC` and `bin/BinToCsv`. The interactive
`Janus_Install.bash` wrapper can install/check the same prerequisites and build
the software.

If JANUS cannot find the locally built FERSlib at runtime, launch it with:

```sh
LD_LIBRARY_PATH=../ferslib/local/lib ./JanusC
```

## Prepare a four-board simulator configuration

Start with the production-shaped JANUS fixture:

```sh
cp pet-caen-daq/test/fixtures/janus/config_same4_v3_good.txt /tmp/janus-simulator-config.txt
```

Replace its four `Open` entries with:

```text
Open[0] eth:127.0.0.1:tdl:0:0
Open[1] eth:127.0.0.1:tdl:1:0
Open[2] eth:127.0.0.1:tdl:2:0
Open[3] eth:127.0.0.1:tdl:3:0
```

Both `eth:` and `usb:` TDL paths ultimately use TCP in FERSlib, but `eth:` is
the clearest description for the loopback simulator.

Set `DataAnalysis DISABLED` when gnuplot is unavailable. Otherwise JANUS opens
a gnuplot pipe and can terminate with SIGPIPE when the missing gnuplot process
exits.

## Run

In one terminal:

```sh
cd pet-caen-daq
task build
./bin/caen-simulator
```

In another terminal, launch JANUS from its `bin` directory because its pixel
map and other resources use relative paths:

```sh
cd janus/Janus_5202_5.0.0_20260713_linux/bin
LD_LIBRARY_PATH=../ferslib/local/lib ./JanusC -c/tmp/janus-simulator-config.txt
```

The `-c` option is concatenated directly with the filename; there is no space
between `-c` and the path.

A successful startup reports the DT5215 identity, one board on each TDlink
0–3, synchronization, four configured boards, and `Ready to start Run`.

For a control/configuration smoke test without generated events:

```sh
./bin/caen-simulator -event-interval 0
```

The synthetic `VERS`, `RBIC`, board BIC, and firmware identities exist only to
satisfy the source-confirmed fields consumed by FERSlib. They are simulator
identity and must not be treated as hardware observations.
