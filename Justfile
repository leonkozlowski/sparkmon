# sparkmon — task runner. Install just: https://github.com/casey/just

# List recipes
default:
    @just --list

# Build the binary to ./bin/sparkmon
build:
    go build -o bin/sparkmon ./cmd/sparkmon

# Run against a config file (config.yaml by default). Pass extra flags: just run -- -config my.yaml
run *ARGS:
    go run ./cmd/sparkmon {{ARGS}}

# Run with nodes given on the command line (no config file needed)
demo HOSTS:
    go run ./cmd/sparkmon -nodes '{{HOSTS}}'

# go mod tidy
tidy:
    go mod tidy

# Format + vet
check:
    go fmt ./...
    go vet ./...

# Build for the DGX Spark nodes (linux/arm64)
build-arm:
    GOOS=linux GOARCH=arm64 go build -o bin/sparkmon-linux-arm64 ./cmd/sparkmon

# Deploy node_exporter + dcgm-exporter to the nodes. Usage: just deploy-exporters "me@host1 me@host2"
deploy-exporters targets:
    go run ./cmd/sparkmon deploy {{targets}}

# Stop the exporter stack on the nodes. Pass --purge to also delete remote dir.
# Usage: just teardown-exporters "me@host1 me@host2"
teardown-exporters targets:
    go run ./cmd/sparkmon teardown {{targets}}

# Probe exporter health. Usage: just health "spark-01 spark-02"
health targets:
    go run ./cmd/sparkmon health {{targets}}

# Build a Debian package. Usage: just deb <path-to-binary> <deb-arch>
# Stages under dist/ (never touches the tracked debian/ sources).
[private]
deb-build binary arch:
    rm -rf dist/deb
    mkdir -p dist/deb/sparkmon/DEBIAN
    mkdir -p dist/deb/sparkmon/usr/bin
    mkdir -p dist/deb/sparkmon/etc/sparkmon
    mkdir -p dist/deb/sparkmon/usr/share/doc/sparkmon
    sed 's/^Architecture: .*/Architecture: {{arch}}/' debian/DEBIAN/control > dist/deb/sparkmon/DEBIAN/control
    cp debian/postinst dist/deb/sparkmon/DEBIAN/postinst
    chmod 755 dist/deb/sparkmon/DEBIAN/postinst
    cp {{binary}} dist/deb/sparkmon/usr/bin/sparkmon
    cp config.yaml.example dist/deb/sparkmon/etc/sparkmon/config.yaml.example
    cp README.md dist/deb/sparkmon/usr/share/doc/sparkmon/README.md
    chmod 755 dist/deb/sparkmon/usr/bin/sparkmon
    chmod 644 dist/deb/sparkmon/etc/sparkmon/config.yaml.example dist/deb/sparkmon/usr/share/doc/sparkmon/README.md
    dpkg-deb --build --root-owner-group dist/deb/sparkmon dist/sparkmon-{{arch}}.deb

# Build a deb for the current machine's architecture
deb: build
    just deb-build bin/sparkmon "$(dpkg --print-architecture 2>/dev/null || echo amd64)"

# Build a deb for the DGX Spark nodes (arm64)
deb-arm: build-arm
    just deb-build bin/sparkmon-linux-arm64 arm64

# Install the deb on this machine (requires sudo)
install-deb: deb
    sudo apt install ./dist/sparkmon-*.deb

clean-deb:
    rm -rf dist/
