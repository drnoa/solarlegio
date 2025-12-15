# SolarManager Legionella Protection

A Go service that ensures your hot water tank is heated to 65°C once a week to prevent legionella growth. Control is handled via the **Solar Manager API**, intelligently utilizing existing PV surplus logic where possible.

## 📋 How it works

The software continuously monitors the hot water temperature. If the target temperature (65°C) is not reached within 7 days (configurable)—neither through solar surplus nor manual heating—the software intervenes.

**The Cascade Logic:**

1.  **Passive Monitoring:**
    * If the water reaches > 65°C due to normal PV surplus charging or standard heating operation, the 7-day timer is reset automatically. No intervention occurs.

2.  **Active Intervention (if necessary):**
    * **Phase 1 (Efficiency):** The Heat Pump (WP) is forced into "ON" mode until 50°C is reached or a timeout occurs.
    * **Phase 2 (Peak Load):** The electrical heating element is forced into "ON" mode until 65°C is reached.
    * **State Restoration:** After each phase, the original device mode (e.g., "Solar Only" or "Off") is restored. The standard Solar Manager logic is not permanently overwritten.

---

## ⚙️ Prerequisites

* Access to a [Solar Manager](https://www.solar-manager.ch/) account.
* Heat Pump and Heating Element integrated into Solar Manager (e.g., via Shelly).
* A server/computer running 24/7 (e.g., Raspberry Pi, Linux VM, NAS with Docker).
* Go (Golang) installed (for compiling).

---

## 🚀 Installation & Configuration

### 1. Build
Compile the program for your platform:

```bash
go build -o solarlegio main.go
````

### 2\. Create Config.json

Create a file named `config.json` in the same directory.
**Note:** You can find the IDs by visiting `https://external-web.solar-manager.ch/v1/info/sensors/{YOUR_SM_ID}` in your browser (while logged in).

```json
{
  "api": {
    "base_url": "[https://external-web.solar-manager.ch/v1](https://external-web.solar-manager.ch/v1)",
    "username": "YOUR_EMAIL",
    "password": "YOUR_PASSWORD"
  },
  "ids": {
    "solar_manager_id": "YOUR_GATEWAY_ID",
    "sensor_temp_id": "TEMP_SENSOR_ID",
    "device_wp_id": "HEATPUMP_SHELLY_ID",
    "device_heater_id": "HEATER_SHELLY_ID"
  },
  "settings": {
    "target_temp_wp": 50.0,
    "target_temp_legio": 65.0,
    "check_interval_minutes": 15,
    "legionella_interval_days": 7,
    "max_duration_hours_wp": 3,
    "max_duration_hours_heater": 3
  }
}
```

-----

## 🐧 Service Setup (Daemon)

To run the program in the background and start it automatically after a reboot (tested on Raspberry Pi OS / Debian / Ubuntu):

### 1\. Place Files

Create a directory `/opt/solarlegio` and move the files there.

```bash
sudo mkdir -p /opt/solarlegio
sudo mv solarlegio /opt/solarlegio/
sudo mv config.json /opt/solarlegio/

# Set permissions (important as the config contains passwords)
sudo chmod 600 /opt/solarlegio/config.json
```

### 2\. Create Systemd Service

Create the service definition file:

```ini
[Unit]
Description=SolarManager Legionella Protection Service
After=network.target

[Service]
# User: Which user should run the service? (often 'root' or 'pi')
# Root is recommended to ensure write permissions for state files,
# otherwise change ownership of /opt/solarlegio accordingly.
User=root

# Working Directory: 'legio_state.json' will be saved here
WorkingDirectory=/opt/solarlegio
ExecStart=/opt/solarlegio/solarlegio

# Restart behavior
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### 3\. Enable and Start Service

```bash
# Reload systemd configuration
sudo systemctl daemon-reload

# Enable autostart on boot
sudo systemctl enable solarlegio

# Start the service now
sudo systemctl start solarlegio
```

### 4\. Check Logs

You can view the live output of the program:

```bash
sudo journalctl -u solarlegio -f
```

-----

## ⚠️ Disclaimer & Safety

  * **API Usage:** This tool uses the internal API of Solar Manager. The API endpoints may change without notice. Use at your own risk.
  * **Hardware:** Ensure your heating element has its own mechanical safety thermostat in case the software control fails (e.g., relay stuck, software crash).
  * **Temperatures:** Verify that your hardware (especially the Heat Pump) is approved for the configured target temperatures.

## 🛠 Troubleshooting

**Error: "state.json: permission denied"**
The service does not have permission to write to the directory.
*Solution:* Check `User=` in the `.service` file or run `chown root:root /opt/solarlegio` (or the corresponding user).

**Error: API Login Failed**
*Solution:* Check Email and Password in `config.json`. Check if 2-Factor Authentication is enabled (this script does not currently support 2FA).
