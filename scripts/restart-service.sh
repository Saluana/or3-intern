#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_dir="${OR3_SERVICE_RUN_DIR:-$repo_root/.run}"
pid_file="$run_dir/or3-intern-service.pid"
log_file="$run_dir/or3-intern-service.log"
env_file="${OR3_ENV_FILE:-$repo_root/.env}"

action="restart"
foreground=false
rebuild=false

usage() {
  cat <<'EOF'
Usage: scripts/restart-service.sh [restart|start|stop|status] [--foreground] [--rebuild]

Defaults:
  action       restart
  mode         background

Options:
  --foreground Keep the service attached to this terminal.
  --rebuild    Rebuild ./or3-intern before starting.

Environment:
  OR3_ENV_FILE         Path to an env file to auto-load before starting.
  OR3_SERVICE_RUN_DIR  Directory for pid/log files.
  OR3_SERVICE_UNSAFE_DEV=true launches `or3-intern --unsafe-dev service`.
EOF
}

for arg in "$@"; do
  case "$arg" in
    restart|start|stop|status)
      action="$arg"
      ;;
    --foreground)
      foreground=true
      ;;
    --rebuild)
      rebuild=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      usage >&2
      exit 1
      ;;
  esac
done

mkdir -p "$run_dir"

load_env_file() {
  if [[ -f "$env_file" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a
  fi
}

service_port() {
  local listen_address="${OR3_SERVICE_LISTEN:-127.0.0.1:9100}"
  printf '%s\n' "${listen_address##*:}"
}

ensure_binary() {
  local binary="$repo_root/or3-intern"
  local needs_rebuild=false

  if [[ "$rebuild" == true || ! -x "$binary" ]]; then
    needs_rebuild=true
  elif find "$repo_root/cmd" "$repo_root/internal" -type f -name '*.go' -newer "$binary" -print -quit | grep -q .; then
    needs_rebuild=true
  elif [[ "$repo_root/go.mod" -nt "$binary" || "$repo_root/go.sum" -nt "$binary" ]]; then
    needs_rebuild=true
  fi

  if [[ "$needs_rebuild" == true ]]; then
    echo "Building or3-intern binary..."
    (cd "$repo_root" && go build -o "$binary" ./cmd/or3-intern)
  fi
}

service_launch_args() {
  local -a args=()
  local unsafe_dev="${OR3_SERVICE_UNSAFE_DEV:-false}"
  unsafe_dev="$(printf '%s' "$unsafe_dev" | tr '[:upper:]' '[:lower:]')"
  case "$unsafe_dev" in
    1|true|yes|on)
      args+=("--unsafe-dev")
      ;;
  esac
  args+=("service")
  printf '%s\n' "${args[@]}"
}

find_service_pids() {
  local pids=()
  local port
  port="$(service_port)"
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    pids+=("$line")
  done < <(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)

  if [[ ${#pids[@]} -eq 0 ]]; then
    local pid command
    pid="$(sed -n '1p' "$pid_file" 2>/dev/null | tr -cd '0-9')"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
      if [[ "$command" == *"or3-intern"* && "$command" == *" service"* ]]; then
        pids+=("$pid")
      fi
    fi
  fi

  if [[ ${#pids[@]} -gt 0 ]]; then
    printf '%s\n' "${pids[@]}" | awk '!seen[$0]++'
  fi
}

wait_for_exit() {
  local pid="$1"
  local attempt
  for attempt in {1..20}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

wait_for_port_release() {
  local port="$1"
  local attempt
  for attempt in {1..40}; do
    if ! lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

stop_service() {
  local allow_replacement="${1:-false}"
  local stopped=false
  local port
  port="$(service_port)"
  local -a stopped_pids=()
  local pid
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    stopped=true
    stopped_pids+=("$pid")
    echo "Stopping or3-intern service (PID $pid)..."
    kill "$pid" 2>/dev/null || true
    if ! wait_for_exit "$pid"; then
      echo "PID $pid did not exit after SIGTERM; sending SIGKILL..."
      kill -9 "$pid" 2>/dev/null || true
    fi
  done < <(find_service_pids)

  if ! wait_for_port_release "$port"; then
    if [[ "$allow_replacement" == true && ${#stopped_pids[@]} -gt 0 ]]; then
      local -a current_pids=()
      local current_pid
      while IFS= read -r current_pid; do
        [[ -n "$current_pid" ]] || continue
        current_pids+=("$current_pid")
      done < <(find_service_pids)

      if [[ ${#current_pids[@]} -gt 0 ]]; then
        local saw_replacement=false
        local old_pid
        for current_pid in "${current_pids[@]}"; do
          local matched_old=false
          for old_pid in "${stopped_pids[@]}"; do
            if [[ "$current_pid" == "$old_pid" ]]; then
              matched_old=true
              break
            fi
          done
          if [[ "$matched_old" == false ]]; then
            saw_replacement=true
            break
          fi
        done

        if [[ "$saw_replacement" == true ]]; then
          echo "A supervisor restarted or3-intern service automatically."
          rm -f "$pid_file"
          return 0
        fi
      fi
    fi

    echo "Port $port is still busy after stopping the service." >&2
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >&2 || true
    return 1
  fi

  rm -f "$pid_file"

  if [[ "$stopped" == false ]]; then
    echo "No running or3-intern service process found."
  fi
}

print_status() {
  local pids=()
  local pid
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    pids+=("$pid")
  done < <(find_service_pids)

  if [[ ${#pids[@]} -eq 0 ]]; then
    echo "or3-intern service is not running."
    echo "Log file: $log_file"
    return 1
  fi

  for pid in "${pids[@]}"; do
    echo "or3-intern service is running as PID $pid"
    ps -p "$pid" -o command=
    lsof -nP -a -p "$pid" -iTCP -sTCP:LISTEN || true
  done
  echo "Log file: $log_file"
}

start_service() {
  load_env_file
  ensure_binary

  local -a launch_args=()
  while IFS= read -r arg; do
    [[ -n "$arg" ]] || continue
    launch_args+=("$arg")
  done < <(service_launch_args)

  if [[ "$foreground" == true ]]; then
    echo "Starting or3-intern service in the foreground..."
    exec "$repo_root/or3-intern" "${launch_args[@]}"
  fi

  echo "Starting or3-intern service in the background..."
  nohup "$repo_root/or3-intern" "${launch_args[@]}" >>"$log_file" 2>&1 &
  local pid=$!
  echo "$pid" > "$pid_file"

  local attempt
  for attempt in {1..40}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "Service exited before becoming ready. Recent log output:" >&2
      tail -n 20 "$log_file" >&2 || true
      return 1
    fi
    if lsof -nP -a -p "$pid" -iTCP -sTCP:LISTEN >/dev/null 2>&1; then
      echo "or3-intern service started as PID $pid"
      echo "Log file: $log_file"
      return 0
    fi
    sleep 0.25
  done

  echo "Service process is running but no listening socket was detected yet. Check $log_file." >&2
  return 1
}

cd "$repo_root"
load_env_file

case "$action" in
  stop)
    stop_service
    ;;
  status)
    print_status
    ;;
  start)
    start_service
    ;;
  restart)
    ensure_binary
    stop_service true
    if [[ -n "$(find_service_pids)" ]]; then
      print_status
      exit 0
    fi
    start_service
    ;;
esac
