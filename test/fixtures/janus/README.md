# JANUS configuration fixtures

## `config_same4_v3_good.txt`

- Evidence classification: production-proven input; its translation to native commands remains to be capture/hardware verified.
- Origin: produced on and supplied from the existing Windows PET CAEN production system.
- Operational history: used for production data acquisition with the Windows build of JANUS 5.0.0.
- Cross-platform expectation: the Windows and Linux JANUS packages have the same version number and are expected to use compatible configuration and hardware protocols. This expectation must still be checked against source, captures, and hardware rather than assumed from the version string alone.
- Topology: four DT5202 boards, one board at node 0 on each of TDlinks 0, 1, 2, and 3.
- DT5215 connection paths: `usb:172.16.0.11:tdl:<chain>:0`, consistent with the manual's Windows USB-gadget default.
- SHA-256: `c472a36aefb2d6956a10bbcaab97515258a7779f96d62caa0e6e39e01e49675d`.
- Original format: ASCII with CRLF line endings; intentionally preserved byte-for-byte.

This is a golden parser and configuration-translation fixture. Tests should copy it to temporary storage when mutation is necessary and must not rewrite the committed file.

The file proves that these settings were accepted and used by JANUS, not yet that every source-derived register value in the native implementation is correct. Future validation should associate this exact configuration hash with:

- JANUS and FERSlib versions;
- DT5215 system/firmware version;
- every DT5202 FPGA and microcontroller firmware version;
- a control/data PCAP;
- JANUS register dumps and decoded run summaries.

The Windows-style `DataFilePath` is part of the original Windows production input and is not a destination for project tests. Tests must override output storage with a temporary directory.
