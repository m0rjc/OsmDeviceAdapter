# LED Matrix Scoreboard Client

Python client for displaying scout patrol scores on a 64x32 LED matrix using the Adafruit RGB Matrix HAT on Raspberry Pi.

## Hardware Requirements

- Raspberry Pi (tested on Pi 3/4)
- [Adafruit RGB Matrix HAT](https://www.adafruit.com/product/2345)
- 64x32 RGB LED Matrix Panel
- 5V 4A power supply for the LED matrix

## Features

- **OAuth Device Flow Authentication** - Displays user code on the matrix for easy authorization
- **Automatic Score Updates** - Polls for patrol scores at configurable intervals
- **Headless Operation** - All user interaction happens on the LED display
- **Error Handling** - Displays errors on the matrix for easy troubleshooting
- **Token Persistence** - Saves authentication token to survive reboots
- **Auto-start** - Can run as a systemd service

## Installation

### 1. Install System Dependencies

```bash
# Update system
sudo apt update
sudo apt upgrade -y

# Install required packages
sudo apt install -y python3 python3-pip python3-dev git

# Install build tools for RGB matrix library
sudo apt install -y build-essential libgraphicsmagick++-dev \
    libwebp-dev libjpeg-dev
```

### 2. Install RGB Matrix Library

The Adafruit RGB Matrix library must be compiled from source:

```bash
# Clone the library
cd ~
git clone https://github.com/hzeller/rpi-rgb-led-matrix.git
cd rpi-rgb-led-matrix

# Build the library
make

# Install Python bindings
cd bindings/python
sudo pip3 install -e .
```

### 3. Install Scoreboard Application

```bash
# Clone this repository (or copy the client-python directory)
cd ~
git clone <repository-url>
cd OsmDeviceAdapter/client-python

# Install Python dependencies
pip3 install -r requirements.txt

# Make the main script executable
chmod +x src/scoreboard.py
```

### 4. Configure the Application

```bash
# Copy example configuration
cp .env.example .env

# Edit configuration
nano .env
```

Update these required settings in `.env`:
- `API_BASE_URL`: URL of your OSM Device Adapter server
- `CLIENT_ID`: Unique identifier for this scoreboard

### 5. Test the Application

Run in simulation mode first (no hardware required):

```bash
cd src
SIMULATE_DISPLAY=true python3 scoreboard.py
```

You should see the device code in the terminal. Visit the verification URL and enter the code to complete authentication.

### 6. Run on Real Hardware

```bash
# Run with hardware
cd src
python3 scoreboard.py
```

The user code will appear on the LED matrix. Visit the URL shown and enter the code.

## Running as a Service

To run the scoreboard automatically at boot:

### 1. Install Service File

```bash
# Copy service file
sudo cp systemd/scoreboard.service /etc/systemd/system/

# Edit service file to match your paths
sudo nano /etc/systemd/system/scoreboard.service
```

### 2. Create Token Directory

```bash
# Create directory for token storage
sudo mkdir -p /var/lib/scoreboard
sudo chown pi:pi /var/lib/scoreboard
```

### 3. Enable and Start Service

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable service to start at boot
sudo systemctl enable scoreboard.service

# Start service now
sudo systemctl start scoreboard.service

# Check status
sudo systemctl status scoreboard.service
```

### 4. View Logs

```bash
# Follow logs in real-time
sudo journalctl -u scoreboard.service -f

# View recent logs
sudo journalctl -u scoreboard.service -n 50
```

## Configuration Options

All configuration is done via environment variables (or `.env` file):

| Variable | Default | Description |
|----------|---------|-------------|
| `API_BASE_URL` | `http://localhost:8080` | OSM Device Adapter server URL |
| `CLIENT_ID` | `scoreboard-rpi` | Unique client identifier |
| `POLL_INTERVAL` | `30` | Seconds between score updates |
| `TOKEN_FILE` | `/var/lib/scoreboard/token.txt` | Where to save OAuth token |
| `SIMULATE_DISPLAY` | `false` | Run without LED hardware |
| `LOG_LEVEL` | `INFO` | Logging level (DEBUG, INFO, WARNING, ERROR) |

## Display Layout

The 64x32 matrix displays up to 4 patrols:

```
Eagles            145
----------------------------
Hawks             132
----------------------------
Wolves            128
----------------------------
Bears             115
```

- Patrol names: Left justified (truncated to 8 characters if longer)
- Scores: Right justified
- Separator lines between patrols

## Troubleshooting

### "rgbmatrix library not available"

The RGB matrix library must be compiled and installed separately. See step 2 of Installation.

### Display is flickering

Adjust the `gpio_slowdown` parameter in `src/display.py`:

```python
options.gpio_slowdown = 4  # Increase if flickering (default: 2)
```

### Permission denied on GPIO

Run with sudo or add your user to the gpio group:

```bash
sudo usermod -a -G gpio pi
```

Then reboot.

### "Authentication expired"

The saved token may have expired. Delete it and re-authenticate:

```bash
rm /var/lib/scoreboard/token.txt
sudo systemctl restart scoreboard.service
```

Watch the matrix for the new device code.

### Can't connect to API server

1. Check `API_BASE_URL` in your `.env` file
2. Verify network connectivity: `ping your-server.example.com`
3. Check firewall rules
4. View logs: `sudo journalctl -u scoreboard.service -n 50`

### Display shows "Score Error"

Check the logs for details:

```bash
sudo journalctl -u scoreboard.service -n 50
```

Common causes:
- Network connectivity issues
- Server is down
- Authentication token expired

## Development

### Testing Without Hardware

Set `SIMULATE_DISPLAY=true` to run without the LED matrix:

```bash
cd src
SIMULATE_DISPLAY=true python3 scoreboard.py
```

Output will be printed to the terminal instead.

### Project Structure

```
client-python/
├── src/
│   ├── scoreboard.py      # Main application
│   ├── api_client.py      # OSM Device Adapter API client
│   └── display.py         # LED matrix display wrapper
├── systemd/
│   └── scoreboard.service # Systemd service file
├── requirements.txt       # Python dependencies
├── .env.example          # Example configuration
└── README.md             # This file
```

## Hardware Setup Notes

### Wiring

The Adafruit RGB Matrix HAT sits directly on the Raspberry Pi GPIO header. No wiring needed.

Connect the LED matrix panel to the HAT's HUB75 connector.

### Power

**Important:** Power the LED matrix from its own 5V power supply, not from the Raspberry Pi.

The Pi can be powered from the HAT's power input or separately via USB.

### Performance

For best performance:
- Use Raspberry Pi 3 or newer
- Disable audio and bluetooth in `/boot/config.txt`:
  ```
  dtparam=audio=off
  dtoverlay=disable-bt
  ```
- Run in console mode (not desktop environment)

## License

See main repository LICENSE file.

## Support

For issues specific to:
- This client: Open an issue in the repository
- RGB Matrix library: See [rpi-rgb-led-matrix](https://github.com/hzeller/rpi-rgb-led-matrix)
- Hardware setup: See [Adafruit's guide](https://learn.adafruit.com/adafruit-rgb-matrix-plus-real-time-clock-hat-for-raspberry-pi)
