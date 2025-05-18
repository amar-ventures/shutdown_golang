#!/bin/bash

# Variables
INSTALL_DIR="$HOME/basescripts/shutdown_golang"
BINARY_NAME="shutdown_daemon_linux"
ENV_FILE=".env"
SERVICE_NAME="shutdown_golang.service"

# Create installation directory
echo "Creating installation directory at $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

# Copy binary and .env file
echo "Copying binary and .env file to $INSTALL_DIR..."
cp "$BINARY_NAME" "$INSTALL_DIR/"
cp "$ENV_FILE" "$INSTALL_DIR/"

# Make the binary executable
chmod +x "$INSTALL_DIR/$BINARY_NAME"

# Create systemd service file
SERVICE_FILE="/etc/systemd/system/$SERVICE_NAME"
echo "Creating systemd service file at $SERVICE_FILE..."
sudo bash -c "cat > $SERVICE_FILE" <<EOL
[Unit]
Description=Shutdown Golang Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# run as root so we can poweroff
User=root
# load your .env
EnvironmentFile=$INSTALL_DIR/$ENV_FILE
ExecStart=$INSTALL_DIR/$BINARY_NAME
WorkingDirectory=$INSTALL_DIR

# allow poweroff
AmbientCapabilities=CAP_SYS_BOOT
CapabilityBoundingSet=CAP_SYS_BOOT
NoNewPrivileges=no

# log to journal
SyslogIdentifier=shutdown_golang
StandardOutput=journal
StandardError=journal

Restart=on-failure
RestartSec=30s

[Install]
WantedBy=multi-user.target
EOL

sudo systemctl daemon-reload
sudo systemctl enable "$SERVICE_NAME"
sudo systemctl restart "$SERVICE_NAME"

# Check service status
echo "Checking service status..."
sudo systemctl status "$SERVICE_NAME"