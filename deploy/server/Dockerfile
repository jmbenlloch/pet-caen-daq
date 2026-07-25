# syntax=docker/dockerfile:1
#
# Package HDF5 artifacts produced by `task docker:build`. Keeping compilation
# in the pinned toolchain makes the packaged server identical to the verified
# production binary while this image retains only its runtime dependencies.
ARG BUILD_IMAGE=pet-caen-daq-build:latest
FROM ${BUILD_IMAGE} AS toolchain

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends zlib1g \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 65532 pet-caen \
    && useradd --system --uid 65532 --gid pet-caen --home-dir /nonexistent pet-caen \
    && install -d -o pet-caen -g pet-caen /etc/pet-caen /var/lib/pet-caen/runs

COPY --from=toolchain /usr/local/hdf5/lib/ /usr/local/hdf5/lib/
COPY --from=toolchain /usr/local/hdf5-blosc/lib/ /usr/local/hdf5-blosc/lib/
COPY --chown=pet-caen:pet-caen bin/pet-caen-daq /usr/local/bin/pet-caen-daq
COPY --chown=pet-caen:pet-caen frontend/dist /opt/pet-caen/frontend
COPY --chown=pet-caen:pet-caen test/fixtures/janus/config_same4_v3_good.txt /etc/pet-caen/config.txt

ENV LD_LIBRARY_PATH="/usr/local/hdf5/lib:/usr/local/hdf5-blosc/lib" \
    HDF5_PLUGIN_PATH="/usr/local/hdf5/lib/plugin"

USER pet-caen
EXPOSE 8080
VOLUME ["/var/lib/pet-caen/runs"]

ENTRYPOINT ["/usr/local/bin/pet-caen-daq"]
CMD ["-config", "/etc/pet-caen/config.txt", "-listen", "0.0.0.0:8080", "-frontend-dir", "/opt/pet-caen/frontend", "-runs", "/var/lib/pet-caen/runs", "-pipeline-capacity", "512", "-authorize-hv-config"]
