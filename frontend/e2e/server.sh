#!/usr/bin/env bash
set -euo pipefail

run_dir=/tmp/pet-caen-playwright-runs
mkdir -p "$run_dir"

simulator_log=/tmp/pet-caen-playwright-simulator.log
../bin/caen-simulator -control 127.0.0.1:19760 -stream 127.0.0.1:19000 >"$simulator_log" 2>&1 &
simulator_pid=$!

cleanup() {
  kill "$simulator_pid" 2>/dev/null || true
  wait "$simulator_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

for _ in {1..100}; do
  if grep -q "CAEN simulator" "$simulator_log"; then
    break
  fi
  if ! kill -0 "$simulator_pid" 2>/dev/null; then
    cat "$simulator_log" >&2
    exit 1
  fi
  sleep 0.05
done

if ! grep -q "CAEN simulator" "$simulator_log"; then
  echo "simulator did not become ready" >&2
  exit 1
fi

../bin/pet-caen-daq \
  -config ../test/fixtures/janus/config_same4_v3_good.txt \
  -control 127.0.0.1:19760 \
  -stream 127.0.0.1:19000 \
  -listen 127.0.0.1:18080 \
  -frontend-dir dist \
  -runs "$run_dir" \
  -authorize-hv-config
