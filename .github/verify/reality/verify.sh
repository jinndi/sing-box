#!/usr/bin/env bash
set -euo pipefail

binary=$(realpath "$1")
script_dir=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
core_pid=
verifier_pid=
request_pid=

cleanup() {
  for pid in "$request_pid" "$core_pid" "$verifier_pid"; do
    if [[ -n "$pid" ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$work"
}
trap cleanup EXIT

go build -o "$work/verify" "$script_dir/main.go"
keypair=$("$binary" generate reality-keypair)
private_key=$(awk '/PrivateKey/ {print $2}' <<<"$keypair")
public_key=$(awk '/PublicKey/ {print $2}' <<<"$keypair")
if [[ -z "$private_key" || -z "$public_key" ]]; then
  echo "Could not generate REALITY key pair" >&2
  exit 1
fi

check_fingerprint() {
  local fingerprint=$1
  local ready_file="$work/verifier-ready"
  rm -f "$ready_file"
  cat >"$work/config.json" <<EOF
{
  "log": {"level": "warn"},
  "inbounds": [{"type": "mixed", "tag": "in", "listen": "127.0.0.1", "listen_port": 18080}],
  "outbounds": [{
    "type": "vless",
    "tag": "reality",
    "server": "127.0.0.1",
    "server_port": 18443,
    "uuid": "11111111-2222-3333-4444-555555555555",
    "flow": "xtls-rprx-vision",
    "tls": {
      "enabled": true,
      "server_name": "www.microsoft.com",
      "utls": {"enabled": true, "fingerprint": "$fingerprint"},
      "reality": {"enabled": true, "public_key": "$public_key", "short_id": "0123abcd"}
    }
  }]
}
EOF

  "$work/verify" -listen 127.0.0.1:18443 -fingerprint "$fingerprint" \
    -private-key "$private_key" -ready-file "$ready_file" &
  verifier_pid=$!
  for _ in $(seq 1 100); do
    [[ -e "$ready_file" ]] && break
    kill -0 "$verifier_pid" 2>/dev/null || { wait "$verifier_pid"; return 1; }
    sleep 0.1
  done
  [[ -e "$ready_file" ]] || { echo "Verifier did not start" >&2; return 1; }

  "$binary" run -c "$work/config.json" >"$work/sing-box.log" 2>&1 &
  core_pid=$!
  local inbound_ready=false
  for _ in $(seq 1 100); do
    if (echo >/dev/tcp/127.0.0.1/18080) >/dev/null 2>&1; then
      inbound_ready=true
      break
    fi
    kill -0 "$core_pid" 2>/dev/null || { cat "$work/sing-box.log" >&2; return 1; }
    sleep 0.1
  done
  if [[ "$inbound_ready" != true ]]; then
    cat "$work/sing-box.log" >&2
    echo "sing-box inbound did not start" >&2
    return 1
  fi

  curl -sS --max-time 10 --proxy http://127.0.0.1:18080 https://example.com -o /dev/null >/dev/null 2>&1 &
  request_pid=$!
  wait "$verifier_pid"
  verifier_pid=
  kill "$core_pid" 2>/dev/null || true
  wait "$core_pid" 2>/dev/null || true
  core_pid=
  wait "$request_pid" 2>/dev/null || true
  request_pid=
}

check_fingerprint chrome
for run in $(seq 1 12); do
  echo "Checking randomized fingerprint, run ${run}/12"
  check_fingerprint randomized
done
