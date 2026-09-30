FROM golang:1.26-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ripgrep python3 nodejs npm git ca-certificates && rm -rf /var/lib/apt/lists/*
ENV HOME=/tmp GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod
WORKDIR /workspace
