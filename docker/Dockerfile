# ============================================================================
#  InfraGraph ASM — Dockerized Build
#  Multi-stage build: compiles Go app + installs exact OSINT tool versions
# ============================================================================

# ── Stage 1: Build InfraGraph binary ────────────────────────────────────────
FROM golang:1.26-bookworm AS builder
# Matches go.mod requirement: go 1.26.4

WORKDIR /src

# Cache dependency download
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /infragraph cmd/infragraph/main.go


# ── Stage 2: Download pre-compiled OSINT tools (pinned versions) ────────────
# Uses pre-built release binaries instead of 'go install' — much faster,
# no need to download hundreds of Go modules.
FROM debian:bookworm-slim AS tools

RUN apt-get update && apt-get install -y --no-install-recommends \
        wget unzip ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /tools

# Subfinder v2.14.0
RUN wget -q "https://github.com/projectdiscovery/subfinder/releases/download/v2.14.0/subfinder_2.14.0_linux_amd64.zip" \
       -O subfinder.zip \
    && unzip -q subfinder.zip -d subfinder_tmp \
    && mv subfinder_tmp/subfinder /tools/subfinder \
    && chmod +x /tools/subfinder \
    && rm -rf subfinder.zip subfinder_tmp

# httpx v1.9.0
RUN wget -q "https://github.com/projectdiscovery/httpx/releases/download/v1.9.0/httpx_1.9.0_linux_amd64.zip" \
       -O httpx.zip \
    && unzip -q httpx.zip -d httpx_tmp \
    && mv httpx_tmp/httpx /tools/httpx \
    && chmod +x /tools/httpx \
    && rm -rf httpx.zip httpx_tmp

# Amass v4.2.0
RUN wget -q "https://github.com/owasp-amass/amass/releases/download/v4.2.0/amass_Linux_amd64.zip" \
       -O amass.zip \
    && unzip -q amass.zip -d amass_tmp \
    && mv amass_tmp/amass_Linux_amd64/amass /tools/amass-v4 \
    && chmod +x /tools/amass-v4 \
    && rm -rf amass.zip amass_tmp


# ── Stage 3: Final minimal runtime image ───────────────────────────────────
FROM debian:bookworm-slim AS runtime

# Labels
LABEL maintainer="InfraGraph ASM"
LABEL description="InfraGraph — Attack Surface Graph Correlator"
LABEL version="3.0"

# Install only the minimal runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        dnsutils \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN useradd -m -s /bin/bash infragraph

# Copy compiled binary from builder stage
COPY --from=builder /infragraph /usr/local/bin/infragraph

# Copy OSINT tools from tools stage
COPY --from=tools /tools/subfinder   /usr/local/bin/subfinder
COPY --from=tools /tools/httpx       /usr/local/bin/httpx
COPY --from=tools /tools/amass-v4    /usr/local/bin/amass-v4

# Copy web frontend
COPY web/ /app/web/

# Create directories for output and config
RUN mkdir -p /app/output /home/infragraph/.config/subfinder \
    && chown -R infragraph:infragraph /app /home/infragraph

WORKDIR /app

# Switch to non-root user
USER infragraph

# Expose the UI/API port
EXPOSE 8080

# Health check — ping the API health endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -sf http://localhost:8080/api/health || exit 1

# Default entrypoint
ENTRYPOINT ["infragraph"]

# Default flags — connect to the companion Neo4j container
CMD ["-serve-only", "-neo4j", "bolt://neo4j:7687", "-web", "/app/web", "-listen", ":8080"]
