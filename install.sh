#!/bin/bash
# Sparkmon one-line installer
# Usage: curl -fsSL https://raw.githubusercontent.com/your-username/sparkmon/main/install.sh | bash
#        curl -sSL your-install-url | bash -- --install-dir /opt/sparkmon
#        curl -sSL your-install-url | bash -- --bin-dir /usr/local/bin

set -e

INSTALL_DIR="${INSTALL_DIR:-/opt/sparkmon}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
BIN_NAME="${BIN_NAME:-sparkmon}"

echo "Installing Sparkmon to $BIN_DIR/sparkmon"
echo "Installation directory: $INSTALL_DIR"

# Download latest release
REPO="your-username/sparkmon"  # Update this
API_URL="https://api.github.com/repos/$REPO/releases/latest"
DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/sparkmon-linux-amd64"

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)
        BINARY_URL="$DOWNLOAD_URL"
        ;;
    aarch64|arm64)
        BINARY_URL="https://github.com/$REPO/releases/latest/download/sparkmon-linux-arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

echo "Detected architecture: $ARCH"
echo "Downloading from: $BINARY_URL"

# Download and install
mkdir -p "$INSTALL_DIR" "$BIN_DIR"
curl -fL "$BINARY_URL" -o "$BIN_NAME.tmp"
chmod +x "$BIN_NAME.tmp"

# Install binary
sudo mv "$BIN_NAME.tmp" "$BIN_DIR/sparkmon"
sudo chown root:root "$BIN_DIR/sparkmon"
sudo chmod 755 "$BIN_DIR/sparkmon"

# Install sample config
if [ ! -f "$INSTALL_DIR/config..yaml.EXAMPLE" ]; then
    echo "Downloading sample config..."
    curl -fL "https://raw.githubusercontent.com/$REPO/main/config.EXAMPLE" \
         -o "$INSTALL_DIR/config.EXAMPLE" || echo "Failed to download config example"
    sudo chown root:root "$INSTALL_DIR/config.EXAMPLE"
    sudo chmod 644 "$INSTALL_DIR/config.EXAMPLE"
fi

echo "Sparkmon installed successfully!"
echo ""
echo "Quick start:"
echo "  1. Copy example config: cp $INSTALL_DIR/config.EXAMPLE ~/.sparkmon/config.EXAMPLE"
echo "  2. Edit config: nano ~/.sparkmon/config.EXAMPLE"
echo "  3. Run sparkmon: sparkmon up"
echo ""
echo "Or deploy exporters to your Spark nodes:"
echo "  sparkmon deploy user@spark-01,user@spark-02"
echo ""
echo "For help: sparkmon --help"