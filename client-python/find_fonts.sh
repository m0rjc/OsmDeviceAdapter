#!/bin/bash
# Script to locate and install BDF fonts for rpi-rgb-led-matrix

echo "Searching for BDF fonts from rpi-rgb-led-matrix..."
echo ""

# Check common locations
COMMON_LOCATIONS=(
    "/usr/local/share/fonts"
    "/usr/share/fonts"
    "$HOME/rpi-rgb-led-matrix/fonts"
    "/opt/rpi-rgb-led-matrix/fonts"
)

FOUND=false
for location in "${COMMON_LOCATIONS[@]}"; do
    if [ -f "$location/7x13.bdf" ]; then
        echo "✓ Found fonts at: $location"
        ls -lh "$location"/*.bdf 2>/dev/null | head -10
        FOUND=true
        break
    fi
done

if [ "$FOUND" = false ]; then
    echo "✗ BDF fonts not found in common locations"
    echo ""
    echo "To install the fonts:"
    echo "1. Clone rpi-rgb-led-matrix if not already done:"
    echo "   git clone https://github.com/hzeller/rpi-rgb-led-matrix.git"
    echo ""
    echo "2. Copy fonts to system location:"
    echo "   sudo mkdir -p /usr/local/share/fonts"
    echo "   sudo cp rpi-rgb-led-matrix/fonts/*.bdf /usr/local/share/fonts/"
    echo ""
    echo "3. Or create a local fonts directory:"
    echo "   mkdir -p $(dirname $0)/fonts"
    echo "   cp rpi-rgb-led-matrix/fonts/*.bdf $(dirname $0)/fonts/"
fi
