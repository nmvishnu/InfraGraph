#!/usr/bin/env bash
# scripts/seed-demo.sh — Populate Neo4j with realistic demo data for UI preview
# Run AFTER Neo4j is started: ./scripts/neo4j.sh start

set -euo pipefail
BOLT_URI="bolt://localhost:7687"
USER="neo4j"
PASS="infragraph"

echo "[seed] Injecting demo ASM data into Neo4j..."

# Use the Go binary's -ingest-only mode with synthetic JSON files
DEMO_DIR="/tmp/infragraph-demo"
mkdir -p "$DEMO_DIR"

# ---- Subfinder demo output ----
cat > "$DEMO_DIR/subfinder.json" << 'EOJSON'
{"host":"mail.acmecorp.io","source":"crtsh","ip":"203.0.113.10"}
{"host":"api.acmecorp.io","source":"crtsh","ip":"203.0.113.10"}
{"host":"dev.acmecorp.io","source":"dnsdumpster","ip":""}
{"host":"staging.acmecorp.io","source":"subfinder","ip":"198.51.100.22"}
{"host":"old-blog.acmecorp.io","source":"crtsh","ip":""}
{"host":"cdn.acmecorp.io","source":"crtsh","ip":"104.21.8.42"}
{"host":"auth.acmecorp.io","source":"subfinder","ip":"203.0.113.10"}
{"host":"status.acmecorp.io","source":"crtsh","ip":"192.168.100.5"}
EOJSON

# ---- Amass demo output ----
cat > "$DEMO_DIR/amass.json" << 'EOJSON'
{"name":"mail.acmecorp.io","domain":"acmecorp.io","addresses":[{"ip":"203.0.113.10","cidr":"203.0.113.0/24","asn":64496}],"tag":"cert","source":"crtsh"}
{"name":"api.acmecorp.io","domain":"acmecorp.io","addresses":[{"ip":"203.0.113.10","cidr":"203.0.113.0/24","asn":64496}],"tag":"cert","source":"crtsh"}
{"name":"staging.acmecorp.io","domain":"acmecorp.io","addresses":[{"ip":"198.51.100.22","cidr":"198.51.100.0/24","asn":64497}],"tag":"dns","source":"dnsdumpster"}
{"name":"old-blog.acmecorp.io","domain":"acmecorp.io","addresses":[],"tag":"cert","source":"crtsh"}
{"name":"cdn.acmecorp.io","domain":"acmecorp.io","addresses":[{"ip":"104.21.8.42","cidr":"104.21.0.0/16","asn":13335}],"tag":"dns","source":"dnsdumpster"}
{"name":"auth.acmecorp.io","domain":"acmecorp.io","addresses":[{"ip":"203.0.113.10","cidr":"203.0.113.0/24","asn":64496}],"tag":"cert","source":"crtsh"}
{"name":"dev.acmecorp.io","domain":"acmecorp.io","addresses":[],"tag":"cert","source":"crtsh"}
EOJSON

# ---- httpx demo output ----
cat > "$DEMO_DIR/httpx.json" << 'EOJSON'
{"url":"https://mail.acmecorp.io","input":"mail.acmecorp.io","title":"Acme Mail - Login","host":"mail.acmecorp.io","ip":"203.0.113.10","status_code":200,"tech":["Roundcube","PHP","Nginx"],"webserver":"nginx/1.18.0","cname":[],"port":"443","scheme":"https"}
{"url":"https://api.acmecorp.io","input":"api.acmecorp.io","title":"Acme API Gateway","host":"api.acmecorp.io","ip":"203.0.113.10","status_code":200,"tech":["Node.js","Express","Nginx"],"webserver":"nginx","cname":[],"port":"443","scheme":"https"}
{"url":"https://staging.acmecorp.io","input":"staging.acmecorp.io","title":"Acme Staging Environment","host":"staging.acmecorp.io","ip":"198.51.100.22","status_code":200,"tech":["React","Webpack","Apache"],"webserver":"Apache/2.4.41","cname":[],"port":"443","scheme":"https"}
{"url":"http://old-blog.acmecorp.io","input":"old-blog.acmecorp.io","title":"","host":"old-blog.acmecorp.io","ip":"","status_code":0,"tech":[],"webserver":"","cname":["acmecorp.ghost.io"],"port":"80","scheme":"http"}
{"url":"https://cdn.acmecorp.io","input":"cdn.acmecorp.io","title":"","host":"cdn.acmecorp.io","ip":"104.21.8.42","status_code":200,"tech":["Cloudflare"],"webserver":"cloudflare","cname":["cdn.acmecorp.io.cdn.cloudflare.net"],"port":"443","scheme":"https"}
{"url":"https://auth.acmecorp.io","input":"auth.acmecorp.io","title":"Acme SSO — Keycloak","host":"auth.acmecorp.io","ip":"203.0.113.10","status_code":200,"tech":["Keycloak","WildFly","Java"],"webserver":"WildFly/23","cname":[],"port":"443","scheme":"https"}
{"url":"http://dev.acmecorp.io","input":"dev.acmecorp.io","title":"","host":"dev.acmecorp.io","ip":"","status_code":0,"tech":[],"webserver":"","cname":["acmecorp.github.io"],"port":"80","scheme":"http"}
EOJSON

echo "[seed] Demo JSON files written to $DEMO_DIR"
echo "[seed] Running infragraph ingest-only..."

/home/kali/InfraGraphASM/build/infragraph \
  -ingest-only \
  -output "$DEMO_DIR" \
  -neo4j bolt://localhost:7687 \
  -user neo4j \
  -pass infragraph \
  -serve-only &

# Give it 3 seconds to ingest, then kill
sleep 3 && kill %1 2>/dev/null || true

echo ""
echo "[seed] Demo data seeded!"
echo "  Dangling DNS targets: old-blog.acmecorp.io (→ ghost.io), dev.acmecorp.io (→ github.io)"
echo "  Shared IP:            203.0.113.10 (mail, api, auth all resolve to same IP)"
echo ""
