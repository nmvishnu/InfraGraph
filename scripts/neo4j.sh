#!/usr/bin/env bash
# InfraGraph — Neo4j standalone start/stop helper
# Usage: ./scripts/neo4j.sh [start|stop|status|reset]

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NEO4J_DIR="$SCRIPT_DIR/../neo4j"

case "${1:-start}" in
  start)
    echo "[neo4j] Starting Neo4j Community Edition..."
    "$NEO4J_DIR/bin/neo4j" start
    echo "[neo4j] Browser UI  → http://localhost:7474"
    echo "[neo4j] Bolt URI    → bolt://localhost:7687"
    echo "[neo4j] Credentials → neo4j / infragraph"
    ;;
  stop)
    echo "[neo4j] Stopping Neo4j..."
    "$NEO4J_DIR/bin/neo4j" stop
    ;;
  status)
    "$NEO4J_DIR/bin/neo4j" status
    ;;
  restart)
    "$NEO4J_DIR/bin/neo4j" restart
    ;;
  reset)
    echo "[neo4j] Stopping and wiping data..."
    "$NEO4J_DIR/bin/neo4j" stop || true
    rm -rf "$NEO4J_DIR/data/databases" "$NEO4J_DIR/data/transactions"
    "$NEO4J_DIR/bin/neo4j-admin" dbms set-initial-password infragraph
    "$NEO4J_DIR/bin/neo4j" start
    echo "[neo4j] Database reset complete."
    ;;
  *)
    echo "Usage: $0 [start|stop|status|restart|reset]"
    exit 1
    ;;
esac
