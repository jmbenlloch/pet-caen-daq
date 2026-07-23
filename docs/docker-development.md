# Docker development workflow

The project build image is the reproducible environment for backend, frontend,
Protobuf/Connect generation, and HDF5 work. It is deliberately a development
image, not yet the production package or runtime image.

## Prerequisite

Install Docker Engine (or another Docker implementation compatible with
`docker build` and `docker run`). No host Go, Node, Buf, Protobuf, HDF5, or
Blosc installation is required. Task is the only optional host tool because it
provides the documented entry points.

## First build

```sh
task docker:image
task docker:verify
```

`docker:image` builds `pet-caen-daq-build:latest`. The slow HDF5 and Blosc
compilation is cached in image layers, so it is repeated only when its
Dockerfile inputs change or the cache is explicitly discarded.

`docker:verify` compiles and runs a native C program that round-trips an HDF5
dataset through Blosc filter 32001, confirms that `h5dump` can read it through
the dynamically loaded plugin, exercises the chosen Go binding, installs pinned
frontend dependencies, and compiles the complete Go and frontend application.

## Daily commands

```sh
task docker:build
task docker:test
task docker:ci
task docker:shell
task docker:run -- go test ./backend/internal/storage
task docker:run -- h5dump --version
```

The wrapper mounts the repository at `/workspace`, uses the caller's numeric
user and group IDs so generated files are not owned by root, and keeps Go and
npm download caches under the ignored `.cache/` directory. Source changes and
build outputs therefore remain on the host.

`docker:run -- ...` is the escape hatch for any command not yet worth a named
Task target. Prefer named targets for stable contributor and CI workflows.

Set `PET_CAEN_BUILD_IMAGE` to use a different local image name:

```sh
PET_CAEN_BUILD_IMAGE=registry.example/pet-caen-build:trial task docker:build
```

## Included toolchain

Versions are explicit build arguments in `deploy/docker/Dockerfile.build`:
Go 1.25.0, Node 24.13.0, Task 3.48.0, Buf 1.65.0, Protobuf 29.3, the Go and
TypeScript Connect/Protobuf generators, HDF5 1.14.6, hdf5-blosc 1.0.1, and
c-blosc 1.21.6. The image also contains compilers, CMake/Ninja, Git, curl,
SQLite, jq, libpcap, tcpdump, and common network diagnostics.

HDF5 is installed under `/usr/local/hdf5`; Blosc libraries are under
`/usr/local/hdf5-blosc`; and the dynamically loaded filter is under
`/usr/local/hdf5/lib/plugin`. `PATH`, `LD_LIBRARY_PATH`, `HDF5_PLUGIN_PATH`,
`CGO_CFLAGS`, and `CGO_LDFLAGS` are already configured for future Go HDF5
bindings. Writers link `libblosc_filter` and call `register_blosc`; readers such
as `h5dump` discover the plugin through `HDF5_PLUGIN_PATH`. This avoids the
legacy plugin's dataset-creation callback incompatibility with HDF5 1.14 while
still producing ordinary filter-32001 files.

The Go integration follows the working muon-veto DAQ precedent and pins
`github.com/next-exp/hdf5-go`. That fork exposes `RegisterBlosc` and
`ConfigureBloscFilter` and links `libblosc_filter` through cgo. Its smoke test
lives under `test/hdf5`, outside `backend`, so ordinary protocol, simulator, and
backend unit tests do not acquire the native dependency. HDF5 adapter tests
must run through `task docker:hdf5` or a broader Docker target.

The initially evaluated fork revision constructed values returned by
`OpenDataset` without the internal datatype that `Close` expected, causing a
panic during close. This was reported and fixed in
[`next-exp/hdf5-go#1`](https://github.com/next-exp/hdf5-go/issues/1). The pinned
revision includes that fix, and the container smoke test covers create, write,
close, reopen, read, and close. Independent readers (`h5dump`, h5py, and the
future schema validator) still remain part of production acceptance.

## Current boundary and packaging

This image is intentionally broad and retains compilers and test tools. When
the HDF5 writer is ready to ship, add a separate multi-stage production
Dockerfile that copies only application binaries and required shared libraries
and plugins. Do not deploy this build image as the DAQ runtime.
