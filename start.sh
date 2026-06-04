#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FMG_HOME="${HOME}/.fmg"
CONFIG_FILE="${FMG_HOME}/config.yaml"
ENV_FILE="${FMG_HOME}/.env"
BINARY="${SCRIPT_DIR}/bin/fmg"
PID_FILE="${FMG_HOME}/fmg.pid"
LOG_DIR="${FMG_HOME}/logs"

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

ensure_home() {
    mkdir -p "${FMG_HOME}/logs"
    if [ ! -f "$CONFIG_FILE" ]; then
        if [ -f "${SCRIPT_DIR}/config.example.yaml" ]; then
            cp "${SCRIPT_DIR}/config.example.yaml" "$CONFIG_FILE"
            log_info "Created config: ${CONFIG_FILE}"
        fi
    fi
    if [ ! -f "$ENV_FILE" ]; then
        if [ -f "${SCRIPT_DIR}/.env.example" ]; then
            cp "${SCRIPT_DIR}/.env.example" "$ENV_FILE"
            log_info "Created env: ${ENV_FILE}"
        fi
    fi
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
        log_warn "Run: make build"
        has_error=1
    else
        local version
        version=$("$BINARY" --version 2>/dev/null || echo "unknown")
        log_info "Binary: ${GREEN}${BINARY}${NC} (${version})"
    fi

    ensure_home

    if [ ! -f "$CONFIG_FILE" ]; then
        log_error "Config not found: ${CONFIG_FILE}"
        has_error=1
    else
        log_info "Config: ${GREEN}${CONFIG_FILE}${NC}"
    fi

    if [ ! -f "$ENV_FILE" ]; then
        log_warn "Env file not found: ${ENV_FILE}"
    else
        log_info "Env: ${GREEN}${ENV_FILE}${NC}"
    fi

    echo ""
    log_step "===== API Key Check ====="
    local key_count=0
    check_key() {
        local name="$1" var="$2"
        if [ -n "${!var:-}" ]; then
            log_info "${name}: ${GREEN}set${NC}"
            key_count=$((key_count + 1))
        else
            log_warn "${name}: env ${var} not set"
        fi
    }
    check_key "OpenCode Zen" "OPENCODE_API_KEY"
    check_key "OpenRouter" "OPENROUTER_API_KEY"
    check_key "AIHubMix" "AIHUBMIX_API_KEY"
    check_key "ZenMux" "ZENMUX_API_KEY"
    echo ""
    if [ "$key_count" -eq 0 ]; then
        log_error "No API keys set; gateway will have no usable models."
        log_warn "Edit ${ENV_FILE} and add your API keys."
        has_error=1
    elif [ "$key_count" -lt 2 ]; then
        log_warn "Only ${key_count}/4 providers have keys; some models will be unavailable"
    else
        log_info "Set ${key_count}/4 provider keys"
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
    "$BINARY" "${args[@]}" >/dev/null 2>&1 &
    local pid=$!
    sleep 1
    if ps -p "$pid" > /dev/null 2>&1; then
        echo ""
        print_banner
        echo -e "${GREEN}${BOLD}FMG started successfully!${NC}"
        echo ""
        echo -e "  Process ID:    ${CYAN}${pid}${NC}"
        echo -e "  Config:        ${CYAN}${CONFIG_FILE}${NC}"
        echo -e "  Log dir:       ${CYAN}${LOG_DIR}${NC}"
        echo -e "  Listen addr:   ${CYAN}http://localhost:10086${NC}"
        echo -e "  Dashboard:     ${CYAN}http://localhost:10086/${NC}"
        echo -e "  API endpoint:  ${CYAN}http://localhost:10086/v1/chat/completions${NC}"
        echo ""
        echo -e "  ${BOLD}Common commands:${NC}"
        echo -e "    Tail logs:   ${CYAN}tail -f ${LOG_DIR}/fmg-$(date +%Y-%m-%d).log${NC}"
        echo -e "    Stop:        ${CYAN}$0 --stop${NC}"
        echo -e "    Restart:     ${CYAN}$0 --stop && $0${NC}"
        echo ""
        if command -v open >/dev/null 2>&1; then
            sleep 1
            open "http://localhost:10086/" 2>/dev/null || true
        fi
    else
        log_error "Startup failed"
        exit 1
    fi
}

main() {
    print_banner
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
