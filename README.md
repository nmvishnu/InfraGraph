<div align="center">

# 🕸️ InfraGraph - Attack Surface Graph Correlator

**Turn hours of manual grep/awk into instant visual attack-path queries.**
  
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Architecture](https://img.shields.io/badge/Binary-100%25%20Static%20Standalone-brightgreen?style=flat-square)](#-build-instructions)
[![SQLite](https://img.shields.io/badge/SQLite-Pure--Go%20Embedded-003B57?style=flat-square&logo=sqlite&logoColor=white)](#-database-management)
[![vis.js](https://img.shields.io/badge/vis.js-Network-f5a623?style=flat-square)](https://visjs.org)
[![License](https://img.shields.io/badge/License-MIT-7c3aed?style=flat-square)](#license)

InfraGraph is an **OSINT / Attack Surface Management** pipeline that embeds `subfinder` and `httpx` directly into a single static binary (Linux & Windows), stores relationships in an **embedded SQLite** engine, and renders it as an interactive **vis.js** network — highlighting **shared infrastructure** and **technology exposure** with zero external dependencies.

---

![InfraGraph UI](docs/screenshots/infragraph-ui.png)

</div>

## 📋 Table of Contents

- [The Problem](#-the-problem)
- [Features](#-features)
- [Demo](#-demo)
- [Installation](#-installation)
- [Building from Source](#-building-from-source)
- [Execution Prerequisites](#-execution-prerequisites)
- [CLI Reference](#-cli-reference)
- [Web UI Guide](#-web-ui-guide)
- [Database Management](#-database-management)
- [Troubleshooting](#-troubleshooting)
- [Security & Ethics](#-security--ethics)
- [Target Users](#-target-users)
- [License](#license)

---

## 🎯 The Problem

Recon tools like Subfinder, Amass, and httpx output **flat text/JSON files**. Red teamers and bug bounty hunters waste hours manually cross-referencing data to find real attack paths:

```
mail.target.com   →  104.21.55.12
api.target.com    →  104.21.55.12                ← 🔗 Shared infra with mail - origin leak!
cdn.target.com    →  Cloudflare (Wappalyzer)     ← ⚙️ Technology exposure
```

**InfraGraph eliminates this** by correlating all findings into a queryable graph and surfacing attack paths through one-click queries.

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| 🚀 **Single Executable** | Self-contained static binary. No Docker, Java, Neo4j, or external Go tool installations required |
| 🔍 **In-Process Recon** | Runs Subfinder and probes with httpx directly within Go runtime |
| ⚡ **Optional Amass** | Automatically detects and uses `amass-v4` if present in PATH; skips gracefully if missing |
| 🕸️ **Graph Correlation** | Relational graph engine mapping domains, IPs, CNAMEs, and technologies |
| 🔗 **Shared Infrastructure** | Discovers IPs serving multiple domains — origin server leaks & tenant co-location |
| ⚙️ **Technology Mapping** | Maps every domain to its detected tech stack (Nginx, React, Cloudflare, etc.) |
| 🌐 **Interactive Web UI** | Dark-mode vis.js graph with click-to-query, search, filter, and hover tooltips |
| 🖥️ **Cross-Platform** | Same unified source code builds natively for both Linux and Windows |
| 📊 **Multiple Output Modes** | Full scan, ingest-only, serve-only, fresh domain re-scan, full reset |

---

## 🎬 Demo

> 🎥 **Demo Video**
![InfraGraph Demo](docs/demo/demo.gif)

<!-- ADDITIONAL SCREENSHOTS -->
<details>
<summary>📸 <strong>Click to expand screenshots</strong></summary>
<br>

> **Graph View** - Full attack surface visualization
> ![Graph View](docs/screenshots/graph-view.png)
> 
> **Shared Infrastructure** - Origin server leak detection
> ![Shared Infra](docs/screenshots/shared-infra.png)
> 
> **Technology Exposure** - Tech stack mapping per domain
> ![Tech Exposure](docs/screenshots/tech-exposure.png)

</details>

---

## 🚀 Installation

### Step 1 — Download the Binary
Download the latest release for your operating system from the **[Releases Page](https://github.com/nmvishnu/InfraGraph/releases)**:
- **Linux:** `infragraph`
- **Windows:** `infragraph.exe`

### Step 2 — Make Executable (Linux only)
If running on Linux, grant execute permissions to the downloaded binary:
```bash
chmod +x infragraph
```

### Step 3 — Run the Tool

**Linux:**
```bash
./infragraph -target example.com
```

**Windows (PowerShell / CMD):**
```powershell
.\infragraph.exe -target example.com
```

Then open **http://localhost:8080** in your browser to view the graph.

> [!NOTE]
> On Windows, you may need to allow the Windows Firewall prompt when prompted to permit opening a local port (`:8080`) to serve the results in your browser.

---

## 🔨 Building from Source

If you prefer to compile from source rather than using the pre-built releases, follow the steps below.

### Build Prerequisites

| Requirement | Details |
|---|---|
| **Go** | Version **1.21+** installed ([go.dev/dl](https://go.dev/dl/)) |
| **Git** | For cloning the repository |

> [!NOTE]
> No C compiler (GCC/Clang), CGO, Java, Docker, or external tools are required to build. All database engines (SQLite) and recon modules (Subfinder, httpx) are written in pure Go.

### 1. Clone the Repository

```bash
git clone https://github.com/nmvishnu/InfraGraph.git
cd InfraGraph
```

### 2. Build Steps

#### 🐧 Building for Linux

```bash
# Using Makefile
make static

# Or directly with Go
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o build/infragraph ./cmd/infragraph
```
Output: `build/infragraph` (fully static ELF binary, zero libc/dynamic dependencies).

#### 🪟 Building for Windows (`.exe`)

You can compile natively on Windows or cross-compile directly from Linux with zero extra tooling:

```bash
# Cross-compiling from Linux:
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o build/infragraph.exe ./cmd/infragraph

# Building natively on Windows (PowerShell / CMD):
go build -trimpath -ldflags="-s -w" -o build/infragraph.exe ./cmd/infragraph
```
Output: `build/infragraph.exe` (standalone Windows console application).

---

## 💻 Execution Prerequisites

Because InfraGraph compiles into a self-contained binary, running the tool requires almost no setup:

| Platform | Requirements |
|---|---|
| **Linux** | Any 64-bit Linux distribution (`chmod +x infragraph`). Web browser for GUI. Internet access for recon. |
| **Windows** | Windows 10/11 or Windows Server (64-bit). PowerShell or CMD. Web browser for GUI. Internet access for recon. |

> [!NOTE]
> **What you DO NOT need:**
> - ❌ No Neo4j or Java runtime
> - ❌ No Docker or Docker Compose
> - ❌ No Subfinder or httpx CLI installations (embedded as Go libraries)
> - ❌ No Visual C++ Redistributable or external DLLs

---

## ⌨️ CLI Reference

```
Usage: infragraph [flags]

Flags:
  -target string     Target domain to enumerate (e.g. example.com)
  -db string         Path to SQLite database file (default: "./infragraph.db")
  -output string     Directory for raw JSON outputs (default: "output")
  -listen string     API/UI server address (default: ":8080")
  -web string        Custom web root directory (uses embedded UI by default)
  -timeout string    Recon timeout in minutes (default: "30")

Modes:
  -ingest-only       Skip recon, only ingest existing JSON files in -output dir
  -serve-only        Skip recon and ingestion, only serve the UI

Data Management:
  -fresh             Delete existing nodes for -target before re-ingesting
  -reset             Wipe the ENTIRE database before ingesting
```

### Usage Examples

**Linux:**
```bash
# Full scan against a target
./build/infragraph -target example.com

# Re-scan a target fresh (wipes old records for this target only)
./build/infragraph -target example.com -fresh

# View existing graph without scanning
./build/infragraph -serve-only

# Ingest existing JSON files without re-scanning
./build/infragraph -ingest-only -output ./output

# Reset entire database and start fresh
./build/infragraph -target example.com -reset
```

**Windows (PowerShell / CMD):**
```powershell
# Full scan against a target
.\build\infragraph.exe -target example.com

# View existing graph without scanning
.\build\infragraph.exe -serve-only

# Reset entire database and scan fresh
.\build\infragraph.exe -target example.com -reset
```

---

## 🌐 Web UI Guide

The web UI is accessible at **http://localhost:8080** after starting InfraGraph.

### Sidebar Query Buttons

| Button | What It Finds |
|--------|---------------|
| 🌐 **Full Graph** | Complete attack surface — all discovered domains, IPs, CNAMEs, and technologies |
| 🔗 **Shared Infrastructure** | IPs serving multiple domains — origin server leaks & shared hosting |
| ⚙️ **Technology Exposure** | Domain → technology stack mapping (Nginx, React, Cloudflare, etc.) |

### Interaction

- **Click** a node to highlight its direct connections
- **Hover** over any node to view detailed metadata in the tooltip
- **Scroll** to zoom in/out
- **Drag** nodes to reposition
- **Search** by domain name using the search bar

---

## 🗄️ Database Management

InfraGraph stores all data in an embedded SQLite database (`infragraph.db` in the working directory).

### Reset Database (Wipe All Scan Data)

```bash
# Option 1 — Using CLI flag
./build/infragraph -target example.com -reset

# Option 2 — Manual removal
rm infragraph.db
```

### Re-scan a Single Target (Preserves Other Targets)

```bash
./build/infragraph -target example.com -fresh
```

### Clean Raw Output Files

```bash
rm -rf output/
```

---

## 🔧 Troubleshooting

<details>
<summary><strong>"0 IPs found" or empty graph</strong></summary>

- Check that output files exist and contain data: `ls -la output/`
- Check database status: `ls -la infragraph.db`
- Try re-ingesting saved files: `./build/infragraph -ingest-only`

</details>

<details>
<summary><strong>UI loads but graph is empty</strong></summary>

Click the **"Full Graph"** button in the sidebar. The graph doesn't auto-load immediately to prevent accidental heavy rendering on large datasets.

</details>

<details>
<summary><strong>Windows Firewall prompt on first run</strong></summary>

When running `infragraph.exe` on Windows for the first time, Windows Defender Firewall may ask for permission. Click **"Allow access"** so the local UI server can accept connections from your browser on port 8080.

</details>

---

## 🔒 Security & Ethics

> [!CAUTION]
> Only scan domains you **own** or have **explicit written permission** to test.

- Respect bug bounty program scope definitions
- Output files in `output/` and `infragraph.db` may contain sensitive infrastructure data — treat accordingly
- API keys stored in `~/.config/subfinder/provider-config.yaml` are sensitive credentials

---

## 🎯 Target Users

| Audience | Use Case |
|----------|----------|
| 🔴 **Red Teams** | Map client external attack surfaces before engagements |
| 🐛 **Bug Bounty Hunters** | Find hidden origin servers and multi-homed infrastructure fast |
| 🏢 **ASM Engineers** | Continuous visibility into forgotten external assets |
| 🎓 **Security Researchers** | Graph-based analysis of internet infrastructure |

---

## 📄 License

This project is licensed under the **MIT License** - see the [LICENSE](LICENSE) file for details.

---

<div align="center">

**Built with ❤️ using Go, Embedded SQLite, and vis.js**

[⬆ Back to Top](#️-infragraph---attack-surface-graph-correlator)

</div>
