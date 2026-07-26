#!/bin/bash
# ============================================================================
#  InfraGraph ASM — Docker Entrypoint Helper
#  Quick commands to build, run, and manage the Dockerized tool
# ============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Auto-detect compose command: v2 plugin vs v1 standalone
if docker compose version &>/dev/null; then
    COMPOSE="docker compose"
elif command -v docker-compose &>/dev/null; then
    COMPOSE="docker-compose"
else
    echo "Error: Neither 'docker compose' nor 'docker-compose' found."
    echo "Install with: sudo apt install docker-compose-plugin"
    exit 1
fi

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

banner() {
    echo -e "${CYAN}"
    echo "╔══════════════════════════════════════════════════════╗"
    echo "║  InfraGraph ASM — Docker Manager                    ║"
    echo "╚══════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

usage() {
    banner
    echo -e "Usage: ${GREEN}./docker/run.sh${NC} <command> [options]"
    echo ""
    echo -e "${YELLOW}Commands:${NC}"
    echo "  build           Build the Docker image"
    echo "  up              Start Neo4j + InfraGraph (UI only)"
    echo "  scan <domain>   Run a full scan against a target domain"
    echo "  ingest          Re-ingest existing output files"
    echo "  down            Stop all containers"
    echo "  logs            Follow container logs"
    echo "  status          Show container status"
    echo "  clean           Remove containers, volumes, and images"
    echo ""
    echo -e "${YELLOW}Examples:${NC}"
    echo "  ./docker/run.sh build"
    echo "  ./docker/run.sh up"
    echo "  ./docker/run.sh scan example.com"
    echo "  ./docker/run.sh scan bsnl.in -timeout 2 -reset"
    echo ""
}

cmd_build() {
    banner
    echo -e "${CYAN}[docker] Building InfraGraph image...${NC}"
    
    # Create subfinder config mount directory if it doesn't exist
    mkdir -p "$SCRIPT_DIR/subfinder-config"
    
    # Copy user's subfinder config if available
    if [ -f "$HOME/.config/subfinder/provider-config.yaml" ]; then
        cp "$HOME/.config/subfinder/provider-config.yaml" "$SCRIPT_DIR/subfinder-config/"
        echo -e "${GREEN}[docker] Copied subfinder API keys from ~/.config/subfinder/${NC}"
    fi
    if [ -f "$HOME/.config/subfinder/config.yaml" ]; then
        cp "$HOME/.config/subfinder/config.yaml" "$SCRIPT_DIR/subfinder-config/"
    fi
    
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" build
    echo -e "${GREEN}[docker] Build complete ✓${NC}"
}

cmd_up() {
    banner
    echo -e "${CYAN}[docker] Starting InfraGraph stack...${NC}"
    mkdir -p "$SCRIPT_DIR/subfinder-config"
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" up -d
    echo ""
    echo -e "${GREEN}[docker] InfraGraph is running ✓${NC}"
    echo -e "  Web UI:       ${CYAN}http://localhost:8080${NC}"
    echo -e "  Neo4j Browser:${CYAN}http://localhost:7474${NC}"
    echo ""
}

cmd_scan() {
    local target="$1"
    shift  # Remove target from args, pass remaining as extra flags
    
    if [ -z "$target" ]; then
        echo -e "${RED}[error] Usage: ./docker/run.sh scan <domain> [flags]${NC}"
        exit 1
    fi
    
    banner
    echo -e "${CYAN}[docker] Running scan against: ${YELLOW}${target}${NC}"
    
    # Ensure stack is up
    mkdir -p "$SCRIPT_DIR/subfinder-config"
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" up -d neo4j
    
    # Wait for Neo4j to be healthy
    echo -e "${CYAN}[docker] Waiting for Neo4j...${NC}"
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" exec neo4j \
        bash -c 'until wget -q --spider http://localhost:7474 2>/dev/null; do sleep 2; done' 2>/dev/null || sleep 10
    
    # Run the scan via docker compose run (one-shot container)
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" run --rm \
        infragraph \
        -target "$target" \
        -neo4j "bolt://neo4j:7687" \
        -web /app/web \
        -output /app/output \
        -listen ":8080" \
        "$@"
}

cmd_ingest() {
    banner
    echo -e "${CYAN}[docker] Re-ingesting existing output files...${NC}"
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" run --rm \
        infragraph \
        -ingest-only \
        -neo4j "bolt://neo4j:7687" \
        -web /app/web \
        -output /app/output
}

cmd_down() {
    banner
    echo -e "${CYAN}[docker] Stopping InfraGraph stack...${NC}"
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" down
    echo -e "${GREEN}[docker] All containers stopped ✓${NC}"
}

cmd_logs() {
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" logs -f
}

cmd_status() {
    banner
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" ps
}

cmd_clean() {
    banner
    echo -e "${RED}[docker] Removing all InfraGraph containers, volumes, and images...${NC}"
    $COMPOSE -f "$SCRIPT_DIR/docker-compose.yml" down -v --rmi all
    echo -e "${GREEN}[docker] Cleaned ✓${NC}"
}

# ── Main ───────────────────────────────────────────────────────────────────
case "${1:-}" in
    build)   cmd_build ;;
    up)      cmd_up ;;
    scan)    shift; cmd_scan "$@" ;;
    ingest)  cmd_ingest ;;
    down)    cmd_down ;;
    logs)    cmd_logs ;;
    status)  cmd_status ;;
    clean)   cmd_clean ;;
    *)       usage ;;
esac
