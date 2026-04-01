#!/usr/bin/env python3
"""
Launch Guard GPIO Example

Monitors the Zaparoo API for staged token notifications (launch guard) and
provides physical feedback via an LED and a confirm button using GPIO pins.

When a token is staged:
  - An LED blinks slowly to indicate a pending confirmation
  - If a delay is configured, the LED switches to solid ON when the delay
    expires (tokens.staged.ready), indicating the button can now be pressed
  - Pressing the confirm button sends the confirm command to the API
  - The LED turns off after confirmation or when the timeout expires

The API confirm bypasses any configured delay, so pressing the button during
the delay period will still work immediately.

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

# GPIO pin assignments (BCM numbering)
LED_PIN = 17
BUTTON_PIN = 27

gpio_available = False
try:
    import RPi.GPIO as GPIO

    gpio_available = True
except ImportError:
    pass


def setup_gpio():
    if not gpio_available:
        return
    GPIO.setmode(GPIO.BCM)
    GPIO.setup(LED_PIN, GPIO.OUT, initial=GPIO.LOW)
    GPIO.setup(BUTTON_PIN, GPIO.IN, pull_up_down=GPIO.PUD_DOWN)


def cleanup_gpio():
    if not gpio_available:
        return
    GPIO.output(LED_PIN, GPIO.LOW)
    GPIO.cleanup()


def set_led(on):
    if gpio_available:
        GPIO.output(LED_PIN, GPIO.HIGH if on else GPIO.LOW)
    state = "ON" if on else "OFF"
    print(f"[LED] {state}")


def read_button():
    if gpio_available:
        return GPIO.input(BUTTON_PIN) == GPIO.HIGH
    return False


def rpc_post(base_url, method, params=None):
    """Send a JSON-RPC 2.0 request via HTTP POST and return the result."""
    body = {"jsonrpc": "2.0", "id": "1", "method": method}
    if params is not None:
        body["params"] = params
    data = json.dumps(body).encode()
    req = urllib.request.Request(
        base_url,
        data=data,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        return json.loads(resp.read())


def get_guard_settings(base_url):
    """Query the API for the launch guard settings."""
    try:
        result = rpc_post(base_url, "settings")
        settings = result.get("result", {})
        return {
            "timeout": settings.get("launchGuardTimeout", 15),
            "delay": settings.get("launchGuardDelay", 0),
        }
    except Exception as e:
        print(f"[API] Failed to get settings: {e}")
        return {"timeout": 15, "delay": 0}


def send_confirm(base_url):
    """Send the confirm command to launch the staged token."""
    try:
        result = rpc_post(base_url, "confirm")
        if "error" in result:
            print(f"[API] Confirm error: {result['error'].get('message', 'unknown')}")
        else:
            print("[API] Confirmed! Token launched.")
    except Exception as e:
        print(f"[API] Confirm failed: {e}")


def wait_for_button(timeout):
    """Poll for a button press within the given timeout (seconds)."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if read_button():
            # Debounce
            time.sleep(0.05)
            if read_button():
                return True
        time.sleep(0.02)
    return False


def blink_led(duration, interval=0.5):
    """Blink the LED for the given duration. Returns True if button pressed."""
    deadline = time.monotonic() + duration
    led_on = False
    while time.monotonic() < deadline:
        led_on = not led_on
        set_led(led_on)
        # Check for button press during blink
        blink_deadline = time.monotonic() + interval
        while time.monotonic() < blink_deadline:
            if read_button():
                time.sleep(0.05)
                if read_button():
                    set_led(True)
                    return True
            time.sleep(0.02)
    set_led(False)
    return False


def run(host, port, no_gpio):
    if not no_gpio:
        setup_gpio()

    base_url = f"http://{host}:{port}/api/v0.1"
    sse_url = f"{base_url}/events"

    settings = get_guard_settings(base_url)
    timeout = settings["timeout"]
    delay = settings["delay"]
    print(f"[CONFIG] Launch guard timeout: {timeout}s, delay: {delay}s")

    print(f"[SSE] Connecting to {sse_url}")

    try:
        while True:
            try:
                req = urllib.request.Request(sse_url)
                with urllib.request.urlopen(req) as resp:
                    print("[SSE] Connected")
                    staged = False

                    for raw_line in resp:
                        line = raw_line.decode().strip()

                        if not line.startswith("data: "):
                            continue

                        msg = json.loads(line[6:])
                        method = msg.get("method")

                        if method == "tokens.staged":
                            params = msg.get("params", {})
                            token_text = params.get("text", "unknown")
                            token_uid = params.get("uid", "")
                            print(f"[STAGED] Token staged: {token_text} (UID: {token_uid})")
                            staged = True

                            if no_gpio:
                                if delay > 0:
                                    print(f"[NO-GPIO] Delay active ({delay}s), waiting for ready...")
                                else:
                                    print(f"[NO-GPIO] Would wait {timeout}s for button press")
                                    print("[NO-GPIO] Simulating button press in 1s...")
                                    time.sleep(1)
                                    send_confirm(base_url)
                                    staged = False
                            else:
                                if delay > 0:
                                    # Blink LED during delay period
                                    print(f"[DELAY] Waiting {delay}s before accepting confirmation...")
                                    pressed = blink_led(delay)
                                    if pressed:
                                        # API confirm bypasses delay
                                        print("[BUTTON] Button pressed during delay (API bypasses delay)")
                                        send_confirm(base_url)
                                        set_led(False)
                                        staged = False
                                    # If not pressed during delay, wait for ready notification
                                else:
                                    # No delay — LED on, wait for button
                                    set_led(True)
                                    pressed = wait_for_button(timeout)
                                    if pressed:
                                        print("[BUTTON] Confirm button pressed!")
                                        send_confirm(base_url)
                                    else:
                                        print("[TIMEOUT] No button press, staged token expired")
                                    set_led(False)
                                    staged = False

                        elif method == "tokens.staged.ready" and staged:
                            print("[READY] Delay expired, ready for confirmation!")

                            if no_gpio:
                                print("[NO-GPIO] Simulating button press in 1s...")
                                time.sleep(1)
                                send_confirm(base_url)
                            else:
                                # Solid LED — ready for confirmation
                                set_led(True)
                                remaining = max(0, timeout - delay)
                                pressed = wait_for_button(remaining)
                                if pressed:
                                    print("[BUTTON] Confirm button pressed!")
                                    send_confirm(base_url)
                                else:
                                    print("[TIMEOUT] No button press, staged token expired")
                                set_led(False)

                            staged = False

            except (ConnectionError, urllib.error.URLError) as e:
                print(f"[SSE] Connection lost ({e}), reconnecting in 2s...")
                time.sleep(2)
                settings = get_guard_settings(base_url)
                timeout = settings["timeout"]
                delay = settings["delay"]

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
