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

# Build debian packages for Ubuntu/Debian
deb:
    mkdir -p debian/sparkmon/usr/bin
    mkdir -p debian/sparkmon/var/lib/sparkmon
    mkdir -p debian/sparkmon/etc/sparkmon
    mkdir -p debian/sparkmon/usr/share/doc/sparkmon
    cp bin/sparkmon debian/sparkmon/usr/bin/sparkmon
    cp config.yaml.example debian/sparkmon/etc/sparkmon/config.yaml.example
    cp README.md debian/sparkmon/usr/share/doc/sparkmon/README.md
    chmod +x debian/sparkmon/usr/bin/sparkmon
    chmod 644 debian/sparkmon/etc/sparkmon/config.yaml.example
    chmod 644 debian/sparkmon/usr/share/doc/sparkmon/README.md
    chown -R root:root debian/sparkmon
    cd debian && dpkg-deb --build sparkmon ../sparkmon.deb

# Install to local system (requires sudo)
install-deb: deb
    sudo apt install -f ./sparkmon.deb || echo "Run: sudo apt install -f ./sparkmon.deb"

# Build for ARM64 and create deb
deb-arm:
    just build-arm
    mkdir -p debian/sparkmon/usr/bin
    mkdir -p debian/sparkmon/var/lib/sparkmon
    mkdir -p debian/sparkmon/etc/sparkmon
    mkdir -p debian/sparkmon/usr/share/doc/sparkmon
    cp bin/sparkmon-*-arm64 debian/sparkmon/usr/bin/sparkmon
    cp config.yaml.example debian/sparkmon/etc/sparkmon/config.yaml.example
    cp README.md debian/sparkmon/usr/share/doc/sparkmon/README.md
    chmod +x debian/sparkmon/usr/bin/sparkmon
    chmod 644 debian/sparkmon/etc/sparkmon/config.yaml.example
    chmod 644 debian/sparkmon/usr/share/doc/sparkmon/README.md
    chown -R root:root debian/sparkmon
    cd debian && dpkg-deb --build sparkmon ../sparkmon-arm64.deb

clean-deb:
    rm -rf debian/
    rm -f *.deb
