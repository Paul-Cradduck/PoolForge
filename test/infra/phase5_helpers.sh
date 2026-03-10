#!/usr/bin/env bash
# Phase 5: EBS volume manipulation helpers for simulating external enclosures
set -euo pipefail

REGION="${REGION:?Set REGION}"

_wait_vol_state() {
  local vid="$1" want="$2" timeout="${3:-120}" elapsed=0
  while (( elapsed < timeout )); do
    state=$(aws ec2 describe-volumes --region "$REGION" --volume-ids "$vid" --query 'Volumes[0].State' --output text)
    [[ "$state" == "$want" ]] && return 0
    sleep 5; (( elapsed += 5 ))
  done
  echo "ERROR: $vid did not reach $want within ${timeout}s" >&2; return 1
}

detach_volume() {
  local iid="$1" vid="$2"
  echo "Detaching $vid from $iid"
  aws ec2 detach-volume --region "$REGION" --instance-id "$iid" --volume-id "$vid"
  _wait_vol_state "$vid" "available"
}

attach_volume() {
  local iid="$1" vid="$2" dev="$3"
  echo "Attaching $vid to $iid at $dev"
  aws ec2 attach-volume --region "$REGION" --instance-id "$iid" --volume-id "$vid" --device "$dev"
  _wait_vol_state "$vid" "in-use"
}

detach_all_pool_volumes() {
  local iid="$1"; IFS=',' read -ra vols <<< "$2"
  for vid in "${vols[@]}"; do detach_volume "$iid" "$vid"; done
}

reattach_all_pool_volumes() {
  local iid="$1"; IFS=',' read -ra vols <<< "$2"; IFS=',' read -ra devs <<< "$3"
  for i in "${!vols[@]}"; do attach_volume "$iid" "${vols[$i]}" "${devs[$i]}"; done
}

simulate_power_cycle() {
  local iid="$1"; IFS=',' read -ra vols <<< "$2"
  local -a devs=()
  for vid in "${vols[@]}"; do
    devs+=($(aws ec2 describe-volumes --region "$REGION" --volume-ids "$vid" \
      --query 'Volumes[0].Attachments[0].Device' --output text))
  done
  detach_all_pool_volumes "$iid" "$2"
  sleep 5
  # Shuffle device assignments
  local -a shuffled; mapfile -t shuffled < <(printf '%s\n' "${devs[@]}" | shuf)
  local csv; csv=$(IFS=','; echo "${shuffled[*]}")
  reattach_all_pool_volumes "$iid" "$2" "$csv"
}

wait_for_device() {
  local dev="$1" timeout="${2:-60}" elapsed=0
  while (( elapsed < timeout )); do
    [[ -b "$dev" ]] && return 0; sleep 1; (( elapsed += 1 ))
  done
  echo "ERROR: $dev not found within ${timeout}s" >&2; return 1
}

collect_phase5_logs() {
  local target="$1" key="$2" dir="$3"
  mkdir -p "$dir"
  local opts=(-i "$key" -o StrictHostKeyChecking=no)
  scp "${opts[@]}" "$target:/etc/mdadm/mdadm.conf" "$dir/mdadm.conf" 2>/dev/null || true
  scp "${opts[@]}" "$target:/var/lib/poolforge/metadata.json" "$dir/metadata.json" 2>/dev/null || true
  ssh "${opts[@]}" "$target" "cat /proc/mdstat" > "$dir/mdstat.txt" 2>/dev/null || true
  ssh "${opts[@]}" "$target" "journalctl -u poolforge --no-pager -n 200" > "$dir/poolforge.log" 2>/dev/null || true
  echo "Phase 5 logs collected in $dir"
}
