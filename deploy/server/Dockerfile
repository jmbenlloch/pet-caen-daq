# syntax=docker/dockerfile:1
#
# Package artifacts produced by `task build`. Keeping compilation outside this
# image makes the packaged server identical to the locally verified binary.
FROM debian:bookworm-slim

RUN groupadd --system --gid 65532 pet-caen \
    && useradd --system --uid 65532 --gid pet-caen --home-dir /nonexistent pet-caen \
    && install -d -o pet-caen -g pet-caen /var/lib/pet-caen/runs

COPY --chown=pet-caen:pet-caen bin/pet-caen-daq /usr/local/bin/pet-caen-daq
COPY --chown=pet-caen:pet-caen frontend/dist /opt/pet-caen/frontend

USER pet-caen
EXPOSE 8080
VOLUME ["/var/lib/pet-caen/runs"]

ENTRYPOINT ["/usr/local/bin/pet-caen-daq"]
CMD ["-config", "/etc/pet-caen/config.txt", "-listen", "0.0.0.0:8080", "-frontend-dir", "/opt/pet-caen/frontend", "-runs", "/var/lib/pet-caen/runs"]
