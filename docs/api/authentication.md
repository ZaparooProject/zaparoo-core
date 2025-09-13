# Zaparoo Core Secure API Authentication

This document provides a complete guide for developers implementing clients that connect to Zaparoo Core using the secure authentication layer.

## Table of Contents

- [Overview](#overview)
- [Security Model](#security-model)
- [Client Pairing](#client-pairing)
- [HTTP API Usage](#http-api-usage)
- [WebSocket API Usage](#websocket-api-usage)
- [Example Implementations](#example-implementations)
- [Error Handling](#error-handling)
- [Security Best Practices](#security-best-practices)

## Overview

Zaparoo Core implements a secure authentication system for remote client connections while maintaining backward compatibility for localhost connections. The security model uses:

- **AES-256-GCM** encryption for all remote API communications
- **Argon2id + HKDF** key derivation for client pairing
- **Sequence numbers + nonces** for replay attack protection
- **Per-client state management** for concurrent access safety

### Connection Types

| Connection Type            | Authentication Required | Encryption Required |
| -------------------------- | ----------------------- | ------------------- |
| Localhost (127.0.0.1, ::1) | ❌ No                   | ❌ No               |
| Remote (all other IPs)     | ✅ Yes                  | ✅ Yes              |

## Security Model

### Encryption Format

All remote API requests must use the following encrypted format:

```json
{
  "encrypted": "base64-encoded-ciphertext",
  "iv": "base64-encoded-initialization-vector",
  "authToken": "client-auth-token"
}
```

### Decrypted Payload Format

The decrypted payload contains the standard JSON-RPC request plus security fields:

```json
{
  "jsonrpc": "2.0",
  "method": "system.version",
  "id": 1,
  "params": {},
  "seq": 123,
  "nonce": "unique-request-nonce"
}
```

## Client Pairing

Before making authenticated requests, clients must complete a pairing process to obtain shared encryption keys.

### Step 1: Initiate Pairing

**Request:**

```http
POST /api/pair/initiate
Content-Type: application/json

{
  "clientName": "MyApp_v1.0"
}
```

**Response:**

```json
{
  "pairingToken": "550e8400-e29b-41d4-a716-446655440000",
  "expiresIn": 300
}
```

### Step 2: Complete Pairing

The client must obtain a verification code through an out-of-band method (QR code, manual entry, etc.) and complete pairing.

**Request:**

```http
POST /api/pair/complete
Content-Type: application/json

{
  "pairingToken": "550e8400-e29b-41d4-a716-446655440000",
  "verifier": "user-provided-verification-code",
  "clientName": "MyApp_v1.0"
}
```

**Response:**

```json
{
  "clientId": "client-uuid-here",
  "authToken": "auth-token-uuid-here",
  "sharedSecret": "64-character-hex-encoded-key"
}
```

### Client Name Requirements

- Length: 1-100 characters
- Characters: letters, numbers, underscore, dash only (`^[a-zA-Z0-9_-]+$`)
- Must be unique per client instance

## HTTP API Usage

### Making Authenticated Requests

1. **Prepare the JSON-RPC payload** with sequence number and nonce
2. **Encrypt the payload** using AES-256-GCM
3. **Send the encrypted request** to the API endpoint

### Example: Get System Version

```javascript
// 1. Prepare JSON-RPC payload
const payload = {
  jsonrpc: "2.0",
  method: "system.version",
  id: 1,
  seq: ++sequenceNumber,
  nonce: generateNonce(),
};

// 2. Encrypt payload
const encrypted = await encryptPayload(JSON.stringify(payload), sharedSecret);

// 3. Send request
const response = await fetch("http://zaparoo-host:7497/api/v0.1", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    encrypted: encrypted.ciphertext,
    iv: encrypted.iv,
    authToken: authToken,
  }),
});

// 4. Decrypt response
const encryptedResponse = await response.json();
const decryptedResponse = await decryptPayload(
  encryptedResponse.encrypted,
  encryptedResponse.iv,
  sharedSecret
);
```

## WebSocket API Usage

WebSocket connections require a two-step authentication process:

### Step 1: Connect and Authenticate

```javascript
const ws = new WebSocket("ws://zaparoo-host:7497/api/v0.1");

ws.onopen = () => {
  // Send authentication message
  ws.send(
    JSON.stringify({
      authToken: authToken,
    })
  );
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);

  if (message.authenticated) {
    console.log("WebSocket authenticated successfully");
    // Now ready to send encrypted requests
  } else {
    // Handle encrypted response
    handleEncryptedMessage(message);
  }
};
```

### Step 2: Send Encrypted Requests

```javascript
async function sendRequest(method, params = {}) {
  const payload = {
    jsonrpc: "2.0",
    method: method,
    id: generateId(),
    params: params,
    seq: ++sequenceNumber,
    nonce: generateNonce(),
  };

  const encrypted = await encryptPayload(JSON.stringify(payload), sharedSecret);

  ws.send(
    JSON.stringify({
      encrypted: encrypted.ciphertext,
      iv: encrypted.iv,
      authToken: authToken,
    })
  );
}

// Example usage
await sendRequest("system.version");
await sendRequest("search.games", { query: "mario" });
```

### Handling Encrypted Responses

```javascript
async function handleEncryptedMessage(encryptedMessage) {
  if (encryptedMessage.encrypted && encryptedMessage.iv) {
    const decrypted = await decryptPayload(
      encryptedMessage.encrypted,
      encryptedMessage.iv,
      sharedSecret
    );

    const response = JSON.parse(decrypted);
    console.log("Received response:", response);
  }
}
```

## Example Implementations

### JavaScript/Node.js Client

```javascript
import crypto from "crypto";

class ZaparooSecureClient {
  constructor(host, port = 7497) {
    this.baseUrl = `http://${host}:${port}`;
    this.wsUrl = `ws://${host}:${port}`;
    this.sequenceNumber = 0;
    this.authToken = null;
    this.sharedSecret = null;
  }

  // Pairing process
  async pair(clientName, verifier) {
    // Step 1: Initiate pairing
    const initResponse = await fetch(`${this.baseUrl}/api/pair/initiate`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ clientName }),
    });

    const { pairingToken } = await initResponse.json();

    // Step 2: Complete pairing (verifier obtained out-of-band)
    const completeResponse = await fetch(`${this.baseUrl}/api/pair/complete`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        pairingToken,
        verifier,
        clientName,
      }),
    });

    const result = await completeResponse.json();
    this.authToken = result.authToken;
    this.sharedSecret = Buffer.from(result.sharedSecret, "hex");

    return result;
  }

  // Encryption utilities
  async encryptPayload(data) {
    const iv = crypto.randomBytes(12);
    const cipher = crypto.createCipherGCM("aes-256-gcm", this.sharedSecret);
    cipher.setAAD(Buffer.alloc(0));

    const encrypted = Buffer.concat([
      cipher.update(data, "utf8"),
      cipher.final(),
    ]);

    const authTag = cipher.getAuthTag();
    const ciphertext = Buffer.concat([encrypted, authTag]);

    return {
      ciphertext: ciphertext.toString("base64"),
      iv: iv.toString("base64"),
    };
  }

  async decryptPayload(encryptedB64, ivB64) {
    const encrypted = Buffer.from(encryptedB64, "base64");
    const iv = Buffer.from(ivB64, "base64");

    const authTag = encrypted.slice(-16);
    const ciphertext = encrypted.slice(0, -16);

    const decipher = crypto.createDecipherGCM("aes-256-gcm", this.sharedSecret);
    decipher.setAuthTag(authTag);
    decipher.setAAD(Buffer.alloc(0));

    const decrypted = Buffer.concat([
      decipher.update(ciphertext),
      decipher.final(),
    ]);

    return decrypted.toString("utf8");
  }

  generateNonce() {
    return crypto.randomBytes(16).toString("hex");
  }

  // HTTP API request
  async request(method, params = {}) {
    const payload = {
      jsonrpc: "2.0",
      method,
      id: Date.now(),
      params,
      seq: ++this.sequenceNumber,
      nonce: this.generateNonce(),
    };

    const encrypted = await this.encryptPayload(JSON.stringify(payload));

    const response = await fetch(`${this.baseUrl}/api/v0.1`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        encrypted: encrypted.ciphertext,
        iv: encrypted.iv,
        authToken: this.authToken,
      }),
    });

    const encryptedResponse = await response.json();
    const decryptedData = await this.decryptPayload(
      encryptedResponse.encrypted,
      encryptedResponse.iv
    );

    return JSON.parse(decryptedData);
  }
}

// Usage example
const client = new ZaparooSecureClient("192.168.1.100");

// Pair with server (verifier obtained from QR code/user input)
await client.pair("MyApp_v1.0", "verification-code-from-qr");

// Make API requests
const version = await client.request("system.version");
const games = await client.request("search.games", { query: "zelda" });
```

### Python Client

```python
import asyncio
import json
import secrets
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
import aiohttp
import websockets

class ZaparooSecureClient:
    def __init__(self, host, port=7497):
        self.base_url = f"http://{host}:{port}"
        self.ws_url = f"ws://{host}:{port}"
        self.sequence_number = 0
        self.auth_token = None
        self.shared_secret = None

    async def pair(self, client_name, verifier):
        async with aiohttp.ClientSession() as session:
            # Initiate pairing
            async with session.post(
                f"{self.base_url}/api/pair/initiate",
                json={"clientName": client_name}
            ) as response:
                result = await response.json()
                pairing_token = result["pairingToken"]

            # Complete pairing
            async with session.post(
                f"{self.base_url}/api/pair/complete",
                json={
                    "pairingToken": pairing_token,
                    "verifier": verifier,
                    "clientName": client_name
                }
            ) as response:
                result = await response.json()
                self.auth_token = result["authToken"]
                self.shared_secret = bytes.fromhex(result["sharedSecret"])
                return result

    def encrypt_payload(self, data):
        aesgcm = AESGCM(self.shared_secret)
        nonce = secrets.token_bytes(12)
        ciphertext = aesgcm.encrypt(nonce, data.encode(), None)

        return {
            "ciphertext": ciphertext.hex(),
            "iv": nonce.hex()
        }

    def decrypt_payload(self, encrypted_hex, iv_hex):
        aesgcm = AESGCM(self.shared_secret)
        ciphertext = bytes.fromhex(encrypted_hex)
        nonce = bytes.fromhex(iv_hex)

        plaintext = aesgcm.decrypt(nonce, ciphertext, None)
        return plaintext.decode()

    def generate_nonce(self):
        return secrets.token_hex(16)

    async def request(self, method, params=None):
        if params is None:
            params = {}

        payload = {
            "jsonrpc": "2.0",
            "method": method,
            "id": 1,
            "params": params,
            "seq": self.sequence_number + 1,
            "nonce": self.generate_nonce()
        }
        self.sequence_number += 1

        encrypted = self.encrypt_payload(json.dumps(payload))

        async with aiohttp.ClientSession() as session:
            async with session.post(
                f"{self.base_url}/api/v0.1",
                json={
                    "encrypted": encrypted["ciphertext"],
                    "iv": encrypted["iv"],
                    "authToken": self.auth_token
                }
            ) as response:
                encrypted_response = await response.json()
                decrypted = self.decrypt_payload(
                    encrypted_response["encrypted"],
                    encrypted_response["iv"]
                )
                return json.loads(decrypted)

# Usage
client = ZaparooSecureClient("192.168.1.100")
await client.pair("MyPythonApp", "verification-code")
result = await client.request("system.version")
```

## Error Handling

### Common Error Responses

| Error                  | HTTP Status | Description                      |
| ---------------------- | ----------- | -------------------------------- |
| Invalid auth token     | 401         | Token not found or expired       |
| Invalid request format | 400         | Malformed JSON or missing fields |
| Decryption failed      | 400         | Invalid encryption or wrong key  |
| Invalid sequence       | 400         | Replay attack detected           |
| Method not allowed     | 405         | Wrong HTTP method                |

### Example Error Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32600,
    "message": "Invalid Request"
  }
}
```

### Client-Side Error Handling

```javascript
try {
  const result = await client.request("system.version");
  console.log(result);
} catch (error) {
  if (error.response?.status === 401) {
    console.log("Authentication failed - re-pair required");
    await client.pair(clientName, newVerifier);
  } else if (error.response?.status === 400) {
    console.log("Request error:", error.response.data);
  } else {
    console.log("Network error:", error.message);
  }
}
```

## Security Best Practices

### For Client Developers

1. **Secure Key Storage**: Store shared secrets securely (keychain, encrypted storage)
2. **Sequence Management**: Persist sequence numbers across app restarts
3. **Nonce Uniqueness**: Generate cryptographically random nonces
4. **Error Handling**: Don't expose sensitive information in logs
5. **Network Security**: Use TLS for additional transport security
6. **Token Rotation**: Implement re-pairing for long-running applications

### Example Secure Storage (Node.js)

```javascript
import keytar from "keytar";

class SecureStorage {
  static async storeCredentials(
    clientId,
    authToken,
    sharedSecret,
    sequenceNumber
  ) {
    await keytar.setPassword("zaparoo", `${clientId}-auth`, authToken);
    await keytar.setPassword("zaparoo", `${clientId}-secret`, sharedSecret);
    await keytar.setPassword(
      "zaparoo",
      `${clientId}-seq`,
      sequenceNumber.toString()
    );
  }

  static async loadCredentials(clientId) {
    const authToken = await keytar.getPassword("zaparoo", `${clientId}-auth`);
    const sharedSecret = await keytar.getPassword(
      "zaparoo",
      `${clientId}-secret`
    );
    const sequenceNumber = parseInt(
      (await keytar.getPassword("zaparoo", `${clientId}-seq`)) || "0"
    );

    return { authToken, sharedSecret, sequenceNumber };
  }
}
```

### Sequence Number Management

```javascript
class SequenceManager {
  constructor(clientId) {
    this.clientId = clientId;
    this.sequenceNumber = this.loadSequenceNumber();
  }

  getNext() {
    this.sequenceNumber++;
    this.saveSequenceNumber();
    return this.sequenceNumber;
  }

  loadSequenceNumber() {
    // Load from persistent storage
    return parseInt(
      localStorage.getItem(`zaparoo-seq-${this.clientId}`) || "0"
    );
  }

  saveSequenceNumber() {
    localStorage.setItem(
      `zaparoo-seq-${this.clientId}`,
      this.sequenceNumber.toString()
    );
  }
}
```

## API Endpoints Reference

### Available Endpoints

| Endpoint                  | Authentication    | Description              |
| ------------------------- | ----------------- | ------------------------ |
| `POST /api/pair/initiate` | None              | Start pairing process    |
| `POST /api/pair/complete` | None              | Complete pairing process |
| `POST /api/v0.1`          | Required (remote) | JSON-RPC API             |
| `WS /api/v0.1`            | Required (remote) | WebSocket API            |

### Supported Methods

Once authenticated, all standard Zaparoo Core JSON-RPC methods are available:

- `system.version` - Get system version
- `system.heartbeat` - Health check
- `search.games` - Search game database
- `launch.game` - Launch a game
- `media.scan` - Trigger media scan
- And more... (see main API documentation)

---

**Note**: This secure authentication layer is only required for remote connections. Localhost connections (127.0.0.1, ::1) continue to work without authentication for development and local tool access.
