# InfraGraph — Build Requirements
# Only needed if you want to BUILD from source.
# If you already have the compiled binary, no other requirements are needed.

# 1. Go 1.21+
#    https://go.dev/dl/

# 2. Build the binary
#    Linux:   CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o build/infragraph ./cmd/infragraph
#    Windows: GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o build/infragraph.exe ./cmd/infragraph

# Optional: Install amass-v4 for deeper subdomain enumeration
#    https://github.com/owasp-amass/amass/releases
#    (Rename binary to amass-v4 and place in PATH)
