#!/bin/bash
# Installation script for LED Matrix Scoreboard

set -e

echo "================================="
echo "LED Matrix Scoreboard Installer"
echo "================================="
echo

# Check if running on Raspberry Pi
if ! grep -q "Raspberry Pi" /proc/cpuinfo 2>/dev/null; then
    echo "WARNING: This doesn't appear to be a Raspberry Pi"
    echo "The RGB matrix library may not work correctly."
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Install system dependencies
echo "Installing system dependencies..."
sudo apt update
sudo apt install -y python3 python3-pip python3-dev git \
    build-essential libgraphicsmagick++-dev libwebp-dev libjpeg-dev

# Install RGB Matrix library
echo
echo "Installing RGB Matrix library..."
if [ ! -d ~/rpi-rgb-led-matrix ]; then
    cd ~
    git clone https://github.com/hzeller/rpi-rgb-led-matrix.git
    cd rpi-rgb-led-matrix
    make
    cd bindings/python
    sudo pip3 install -e .
    echo "RGB Matrix library installed"
else
    echo "RGB Matrix library already exists, skipping..."
fi

# Install Python dependencies
echo
echo "Installing Python dependencies..."
cd "$(dirname "$0")"
pip3 install -r requirements.txt

# Create configuration file
echo
if [ ! -f .env ]; then
    echo "Creating configuration file..."
    cp .env.example .env
    echo "Configuration file created at .env"
    echo "IMPORTANT: Edit .env and set your API_BASE_URL"
else
    echo "Configuration file already exists, skipping..."
fi

# Create token directory
echo
echo "Creating token storage directory..."
sudo mkdir -p /var/lib/scoreboard
sudo chown $USER:$USER /var/lib/scoreboard
echo "Token directory created"

# Make scripts executable
echo
echo "Setting permissions..."
chmod +x src/scoreboard.py

# Test installation
echo
echo "Testing installation..."
if python3 -c "import requests" 2>/dev/null; then
    echo "✓ Python dependencies OK"
else
    echo "✗ Python dependencies failed"
    exit 1
fi

echo
echo "================================="
echo "Installation complete!"
echo "================================="
echo
echo "Next steps:"
echo "1. Edit .env and set your API_BASE_URL"
echo "2. Test in simulation mode:"
echo "   cd src && SIMULATE_DISPLAY=true python3 scoreboard.py"
echo "3. Install as a service (optional):"
echo "   sudo cp systemd/scoreboard.service /etc/systemd/system/"
echo "   sudo nano /etc/systemd/system/scoreboard.service  # Edit paths"
echo "   sudo systemctl daemon-reload"
echo "   sudo systemctl enable scoreboard.service"
echo "   sudo systemctl start scoreboard.service"
echo
