#!/bin/bash
# ============================================================
#  Free Model Gateway (FMG) - One-click launch script
#  Usage: ./start.sh [--dev] [--check] [--stop]
# ============================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/config.yaml"
BINARY="${SCRIPT_DIR}/bin/fmg"
PID_FILE="${SCRIPT_DIR}/fmg.pid"
LOG_FILE="${SCRIPT_DIR}/logs/fmg.log"

MODE="production"
ACTION="start"
LOG_LEVEL="info"

for arg in "$@"; do
    case $arg in
        --dev) MODE="dev"; LOG_LEVEL="debug"; shift ;;
        --check) ACTION="check"; shift ;;
        --stop) ACTION="stop"; shift ;;
        *) echo "Unknown arg: $arg"; echo "Usage: $0 [--dev] [--check] [--stop]"; exit 1 ;;
    esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }

print_banner() {
    echo -e "${CYAN}${BOLD}"
    echo "╔══════════════════════════════════════════╗"
    echo "║       Free Model Gateway v1.0.0          ║"
    echo "║      AI 模型智能路由网关                  ║"
    echo "╚══════════════════════════════════════════╝"
    echo -e "${NC}"
}

stop_instance() {
    if [ ! -f "$PID_FILE" ]; then
        log_warn "No running instance found (PID file missing)"
        return 0
    fi
    PID=$(cat "$PID_FILE")
    if ps -p "$PID" > /dev/null 2>&1; then
        log_info "Stopping instance (PID: ${PID})..."
        kill "$PID" 2>/dev/null || true
        for i in $(seq 1 5); do
            if ! ps -p "$PID" > /dev/null 2>&1; then
                log_info "Instance stopped"
                rm -f "$PID_FILE"
                return 0
            fi
            sleep 1
        done
        log_warn "Graceful stop failed; force killing..."
        kill -9 "$PID" 2>/dev/null || true
        rm -f "$PID_FILE"
        log_info "Instance force-stopped"
    else
        log_warn "Process ${PID} does not exist; cleaning up stale PID file"
        rm -f "$PID_FILE"
    fi
}

check_environment() {
    local has_error=0
    echo ""
    log_step "===== Environment Check ====="

    if [ ! -f "$BINARY" ]; then
        log_error "Binary not found: ${BINARY}"
        log_warn "Run: make build   OR   go build -o bin/fmg ./cmd/fmg/"
        has_error=1
    else
        local version
        version=$("$BINARY" --version 2>/dev/null || echo "unknown")
        log_info "Binary: ${GREEN}${BINARY}${NC} (${version})"
    fi

    if [ ! -f "$CONFIG_FILE" ]; then
        log_error "Config not found: ${CONFIG_FILE}"
        log_warn "Run: cp config.example.yaml config.yaml"
        has_error=1
    else
        log_info "Config: ${GREEN}${CONFIG_FILE}${NC}"
        if [ ! -s "$CONFIG_FILE" ]; then
            log_error "Config is empty!"
            has_error=1
        fi
    fi

    mkdir -p "$(dirname "$LOG_FILE")"

    echo ""
    log_step "===== API Key Check ====="
    declare -A KEY_MAP=(
        ["OPENCODE_API_KEY"]="OpenCode Zen"
        ["OPENROUTER_API_KEY"]="OpenRouter"
        ["AIHUBMIX_API_KEY"]="AIHubMix"
        ["KILO_API_KEY"]="Kilo Gateway"
        ["ZENMUX_API_KEY"]="ZenMux"
    )
    local key_count=0
    for env_var in "${!KEY_MAP[@]}"; do
        if [ -n "${!env_var:-}" ]; then
            log_info "${KEY_MAP[$env_var]}: ${GREEN}set${NC}"
            key_count=$((key_count + 1))
        else
            log_warn "${KEY_MAP[$env_var]}: env ${env_var} not set"
        fi
    done
    echo ""
    if [ "$key_count" -eq 0 ]; then
        log_error "No API keys set; gateway will have no usable models."
        log_warn "Set env vars or create .env:"
        echo "  cat > ${SCRIPT_DIR}/.env << 'EOF'"
        for env_var in "${!KEY_MAP[@]}"; do
            echo "export ${env_var}=your-key-here"
        done
        echo "EOF"
        has_error=1
    elif [ "$key_count" -lt 3 ]; then
        log_warn "Only ${key_count}/5 providers have keys; some models will be unavailable"
    else
        log_info "Set ${key_count}/5 provider keys"
    fi

    echo ""
    log_step "===== Port Check ====="
    local PORT=10086
    if command -v lsof >/dev/null 2>&1; then
        if lsof -i :"$PORT" >/dev/null 2>&1; then
            log_warn "Port ${PORT} appears to be in use:"
            lsof -i :"$PORT" | head -5 || true
            log_warn "If this is a stale instance, run: $0 --stop"
        else
            log_info "Port ${PORT}: ${GREEN}available${NC}"
        fi
    else
        log_info "Port ${PORT}: (skipped, lsof not available)"
    fi
    return $has_error
}

load_env_file() {
    local env_file="${SCRIPT_DIR}/.env"
    if [ -f "$env_file" ]; then
        log_info "Loading .env file: ${env_file}"
        set -a
        # shellcheck source=/dev/null
        source "$env_file"
        set +a
    fi
}

start_service() {
    if [ -f "$PID_FILE" ]; then
        local old_pid
        old_pid=$(cat "$PID_FILE")
        if ps -p "$old_pid" > /dev/null 2>&1; then
            log_error "Instance already running (PID: ${old_pid})"
            log_warn "Run: $0 --stop"
            exit 1
        else
            log_warn "Removing stale PID file"
            rm -f "$PID_FILE"
        fi
    fi

    local args=("-c" "$CONFIG_FILE" "-l" "$LOG_LEVEL")
    if [ "$MODE" = "dev" ]; then
        log_info "Mode: ${YELLOW}dev (debug logs)${NC}"
    else
        log_info "Mode: ${GREEN}production${NC}"
    fi

    echo ""
    log_step "===== Starting Free Model Gateway ====="
    echo ""
    "$BINARY" "${args[@]}" >> "$LOG_FILE" 2>&1 &
    local pid=$!
    echo "$pid" > "$PID_FILE"
    sleep 1
    if ps -p "$pid" > /dev/null 2>&1; then
        echo ""
        print_banner
        echo -e "${GREEN}${BOLD}FMG started successfully!${NC}"
        echo ""
        echo -e "  Process ID:    ${CYAN}${pid}${NC}"
        echo -e "  Listen addr:   ${CYAN}http://localhost:10086${NC}"
        echo -e "  API endpoint:  ${CYAN}http://localhost:10086/v1/chat/completions${NC}"
        echo -e "  Models list:   ${CYAN}http://localhost:10086/v1/models${NC}"
        echo -e "  Health:        ${CYAN}http://localhost:10086/health${NC}"
        echo -e "  Stats:         ${CYAN}http://localhost:10086/stats${NC}"
        echo -e "  Log file:      ${CYAN}${LOG_FILE}${NC}"
        echo -e "  PID file:      ${CYAN}${PID_FILE}${NC}"
        echo ""
        echo -e "  ${BOLD}Common commands:${NC}"
        echo -e "    Tail logs:   ${CYAN}tail -f ${LOG_FILE}${NC}"
        echo -e "    Stop:        ${CYAN}$0 --stop${NC}"
        echo -e "    Restart:     ${CYAN}$0 --stop && $0${NC}"
        echo -e "    Test:        ${CYAN}curl http://localhost:10086/health${NC}"
        echo ""
        if command -v open >/dev/null 2>&1; then
            sleep 1
            open "http://localhost:10086/health" 2>/dev/null || true
        elif command -v xdg-open >/dev/null 2>&1; then
            xdg-open "http://localhost:10086/health" 2>/dev/null || true
        fi
    else
        log_error "Startup failed; tail of log:"
        echo ""
        tail -20 "$LOG_FILE" 2>/dev/null || echo "(no log output)"
        exit 1
    fi
}

main() {
    print_banner
    load_env_file
    case "$ACTION" in
        stop) stop_instance; exit 0 ;;
        check)
            check_environment
            local rc=$?
            if [ $rc -eq 0 ]; then
                echo ""
                log_info "${GREEN}Environment check passed${NC}"
            else
                echo ""
                log_error "${RED}Environment check failed${NC}"
            fi
            exit $rc
            ;;
        start)
            if ! check_environment; then
                echo ""
                log_error "Environment check failed; continue anyway? (y/N)"
                read -r ans
                if [ "$ans" != "y" ] && [ "$ans" != "Y" ]; then
                    log_info "Startup cancelled"
                    exit 1
                fi
            fi
            start_service
            ;;
    esac
}

main "$@"
