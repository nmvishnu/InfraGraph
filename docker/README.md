# InfraGraph ASM — Docker Deployment

## Quick Start

```bash
# 1. Build the image
./docker/run.sh build

# 2. Start the full stack (Neo4j + InfraGraph UI)
./docker/run.sh up

# 3. Open the UI
# http://localhost:8080
```

## Running a Scan

```bash
# Full scan with default timeout (30 min)
./docker/run.sh scan example.com

# Scan with 2-minute timeout and database reset
./docker/run.sh scan bsnl.in -timeout 2 -reset

# Re-ingest existing output files
./docker/run.sh ingest
```

## All Commands

| Command | Description |
|---------|-------------|
| `./docker/run.sh build` | Build the Docker image |
| `./docker/run.sh up` | Start Neo4j + InfraGraph (UI only) |
| `./docker/run.sh scan <domain>` | Run a full recon scan |
| `./docker/run.sh ingest` | Re-ingest existing JSON outputs |
| `./docker/run.sh down` | Stop all containers |
| `./docker/run.sh logs` | Follow live container logs |
| `./docker/run.sh status` | Show container status |
| `./docker/run.sh clean` | Remove everything (containers + volumes + images) |

## Pinned Tool Versions

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.26 (Docker base) | Compiles InfraGraph binary |
| Neo4j | 5.26.8 Community | Graph database backend |
| Subfinder | v2.14.0 | Passive subdomain enumeration |
| httpx | v1.9.0 | HTTP probing, tech detection, IP/CNAME resolution |
| Amass | v4.2.0 | Deep subdomain enumeration |
| neo4j-go-driver | v5.28.4 | Go ↔ Neo4j communication |

## Subfinder API Keys (Optional)

To use Subfinder with your own API keys (for better results), place your
`provider-config.yaml` in `docker/subfinder-config/` before building:

```bash
cp ~/.config/subfinder/provider-config.yaml docker/subfinder-config/
```

The `run.sh build` command does this automatically if the file exists.

## Architecture

```
┌─────────────────────────────────────────────────┐
│  Docker Network: infragraph-net                 │
│                                                 │
│  ┌──────────────┐      ┌────────────────────┐   │
│  │  neo4j:7687  │◄────►│  infragraph:8080   │   │
│  │  (bolt)      │      │  ┌──────────────┐  │   │
│  │              │      │  │  subfinder    │  │   │
│  │  neo4j:7474  │      │  │  httpx        │  │   │
│  │  (browser)   │      │  │  amass-v4     │  │   │
│  └──────────────┘      │  └──────────────┘  │   │
│        │               │        │           │   │
│    neo4j_data          │   infragraph_output │   │
│    neo4j_logs          └────────────────────┘   │
└─────────────────────────────────────────────────┘
```

## Ports

| Port | Service |
|------|---------|
| `8080` | InfraGraph Web UI + API |
| `7474` | Neo4j Browser UI |
| `7687` | Neo4j Bolt Protocol |
