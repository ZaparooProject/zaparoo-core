#!/usr/bin/env python3
"""
Launch Guard GPIO Example

Monitors the Zaparoo API for staged token notifications (launch guard) and
provides physical feedback via an LED and a confirm button using GPIO pins.

When a token is staged:
  - The LED turns on to indicate a pending confirmation
  - Pressing the confirm button sends the confirm command to the API
  - The LED turns off after confirmation or timeout

API confirm bypasses any configured delay, so the button always works
immediately regardless of launch guard delay settings.

This script runs on a Raspberry Pi (or similar SBC with GPIO) and connects
to Zaparoo over the network. The Zaparoo device can be a MiSTer, Batocera,
SteamOS, or any other supported platform.

Uses SSE (Server-Sent Events) for notifications and HTTP POST for commands.
Only requires the Python standard library and RPi.GPIO (pre-installed on
Raspberry Pi OS).

Wiring:
  - LED on GPIO 17 (BCM) with a current-limiting resistor to GND
  - Momentary button on GPIO 27 (BCM) with a pull-down resistor to GND

Usage:
  python3 launch_guard_gpio.py --host 10.0.0.107
  python3 launch_guard_gpio.py --no-gpio  # test without hardware
"""

import argparse
import json
import sys
import time
import urllib.request

# --- Configuration ---
LED_PIN = 17  # GPIO pin for the LED (BCM numbering)
BUTTON_PIN = 27  # GPIO pin for the confirm button (BCM numbering)
BUTTON_TIMEOUT = 30  # Seconds to wait for a button press before giving up

gpio_available = False
try:
    import RPi.GPIO as GPIO

    gpio_available = True
except ImportError:
    pass


def setup_gpio():
    GPIO.setmode(GPIO.BCM)
    GPIO.setup(LED_PIN, GPIO.OUT, initial=GPIO.LOW)
    GPIO.setup(BUTTON_PIN, GPIO.IN, pull_up_down=GPIO.PUD_DOWN)


def cleanup_gpio():
    GPIO.output(LED_PIN, GPIO.LOW)
    GPIO.cleanup()


def set_led(on):
    if gpio_available:
        GPIO.output(LED_PIN, GPIO.HIGH if on else GPIO.LOW)
    print(f"[LED] {'ON' if on else 'OFF'}")


def wait_for_button():
    """Poll for a button press. Returns True if pressed within timeout."""
    deadline = time.monotonic() + BUTTON_TIMEOUT
    while time.monotonic() < deadline:
        if gpio_available and GPIO.input(BUTTON_PIN) == GPIO.HIGH:
            time.sleep(0.05)  # debounce
            if GPIO.input(BUTTON_PIN) == GPIO.HIGH:
                return True
        time.sleep(0.02)
    return False


def send_confirm(base_url):
    """Send the confirm command to launch the staged token."""
    body = json.dumps({"jsonrpc": "2.0", "id": "1", "method": "confirm"}).encode()
    req = urllib.request.Request(
        base_url, data=body, headers={"Content-Type": "application/json"}
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            result = json.loads(resp.read())
            if "error" in result:
                print(f"[API] Confirm error: {result['error'].get('message')}")
            else:
                print("[API] Confirmed! Token launched.")
    except Exception as e:
        print(f"[API] Confirm failed: {e}")


def run(host, port, no_gpio):
    if not no_gpio:
        setup_gpio()

    base_url = f"http://{host}:{port}/api/v0.1"
    sse_url = f"{base_url}/events"

    print(f"[SSE] Connecting to {sse_url}")

    try:
        while True:
            try:
                req = urllib.request.Request(sse_url)
                with urllib.request.urlopen(req) as resp:
                    print("[SSE] Connected, waiting for staged tokens...")

                    for raw_line in resp:
                        line = raw_line.decode().strip()
                        if not line.startswith("data: "):
                            continue

                        msg = json.loads(line[6:])
                        if msg.get("method") != "tokens.staged":
                            continue

                        # A token was staged — wait for button press to confirm
                        params = msg.get("params", {})
                        print(f"[STAGED] {params.get('text', '?')} (UID: {params.get('uid', '?')})")

                        set_led(True)

                        if no_gpio:
                            print("[NO-GPIO] Simulating button press in 1s...")
                            time.sleep(1)
                            pressed = True
                        else:
                            pressed = wait_for_button()

                        if pressed:
                            print("[BUTTON] Confirm button pressed!")
                            send_confirm(base_url)
                        else:
                            print("[TIMEOUT] No button press, giving up")

                        set_led(False)

            except (ConnectionError, urllib.error.URLError) as e:
                print(f"[SSE] Connection lost ({e}), reconnecting in 2s...")
                time.sleep(2)

    except KeyboardInterrupt:
        print("\n[EXIT] Shutting down")
    finally:
        set_led(False)
        if not no_gpio:
            cleanup_gpio()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Launch Guard GPIO Monitor")
    parser.add_argument("--host", default="localhost", help="Zaparoo API host")
    parser.add_argument("--port", type=int, default=7497, help="Zaparoo API port")
    parser.add_argument(
        "--no-gpio",
        action="store_true",
        help="Run without GPIO hardware (simulates button press)",
    )
    args = parser.parse_args()

    if not args.no_gpio and not gpio_available:
        print("RPi.GPIO not available. Use --no-gpio to run without hardware.")
        sys.exit(1)

    run(args.host, args.port, args.no_gpio)
