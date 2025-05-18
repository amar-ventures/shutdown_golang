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
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$BINARY_NAME
Restart=always
User=$USER
EnvironmentFile=$INSTALL_DIR/$ENV_FILE
WorkingDirectory=$INSTALL_DIR

[Install]
WantedBy=multi-user.target
EOL

# Reload systemd, enable and start the service
echo "Reloading systemd, enabling, and starting the service..."
sudo systemctl daemon-reload
sudo systemctl enable "$SERVICE_NAME"
sudo systemctl start "$SERVICE_NAME"

# Check service status
echo "Checking service status..."
sudo systemctl status "$SERVICE_NAME"