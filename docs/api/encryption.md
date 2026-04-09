# API Encryption

> **Status**: Available in Zaparoo Core 2.x. Disabled by default.

The Zaparoo API supports application-layer encryption on the WebSocket transport using a PAKE-based pairing flow and AES-256-GCM with per-session HKDF-derived keys. This document describes the wire protocol for client SDK implementors.

## Overview

- **Pairing**: One-time PAKE2 (P-256) handshake. The user enters a 6-digit PIN displayed on the Zaparoo device into the client app. A shared 32-byte pairing key is derived without ever transmitting the PIN.
- **Encryption**: AES-256-GCM with implicit counter-derived nonces on every WebSocket frame after the first.
- **Per-session keys**: On each new WebSocket connection the client generates a random 16-byte session salt; both sides derive ephemeral session keys via HKDF-SHA256 using the pairing key as IKM and the session salt as the HKDF salt. Counters reset to 0 each session.
- **Scope**: Encryption applies to **WebSocket only**. Non-WebSocket transports (HTTP POST, SSE, REST GET) are restricted to localhost by default and require an explicit IP allowlist for remote access.

## Encryption setting

The server has a single boolean setting `encryption` (in the `[service]` section of `config.toml`):

| Value | Behavior |
|---|---|
| `false` (default) | No encryption. All WebSocket connections accepted as plaintext; API key authentication applies. |
| `true` | Remote WebSocket connections must send an encrypted first frame derived from a paired key. Plaintext WebSocket connections from non-loopback addresses are rejected. API key authentication for the WebSocket transport is bypassed (the pairing key proves identity). Localhost is always exempt — local plaintext WebSocket connections continue to work without pairing. |

The `encryption` setting only affects the WebSocket transport. HTTP POST, SSE, and REST GET endpoints always use the IP allowlist + API key auth model and are unaffected.

## Pairing flow

Pairing happens over plain HTTP (the PAKE protocol provides its own security). Two round trips establish a shared 32-byte pairing key.

```text
TUI                     Server                       Client App
───                     ──────                       ──────────
User: "Pair device"
                        Generate 6-digit PIN
Display PIN             Store PIN (5min expiry)

                                                     User types PIN
                                                     A = pake.InitCurve(pin, 0, "p256")

                        ← POST /api/pair/start ────────────────
                          { "pake": base64(A.Bytes()),
                            "name": "MyApp" }

                        B = pake.InitCurve(pin, 1, "p256")
                        B.Update(A.Bytes())

                        ── 200 { "session": uuid, ──────────→
                             "pake": base64(B.Bytes()) }

                                                     A.Update(B.Bytes())
                                                     sessionKey = A.SessionKey()
                                                     prk = HKDF-Extract(sessionKey, nil)
                                                     confirmKeyA = HKDF-Expand(prk, "zaparoo-confirm-A", 32)
                                                     confirmKeyB = HKDF-Expand(prk, "zaparoo-confirm-B", 32)
                                                     pairingKey = HKDF-Expand(prk, "zaparoo-pairing-v1", 32)

                        ← POST /api/pair/finish ───────────────
                          { "session": uuid,
                            "confirm": base64(HMAC-SHA256(
                              confirmKeyA,
                              LP("zaparoo-v1") || LP("p256") ||
                              LP("client") || LP(name) ||
                              LP(MsgA) || LP(MsgB))) }

                        Verify client HMAC.
                        On success: persist client, return:

                        ── 200 { "authToken": uuid, ────────→
                             "clientId": uuid,
                             "confirm": base64(HMAC(
                               confirmKeyB, "server" || ...)) }

                                                     Verify server HMAC.
                                                     Store authToken + pairingKey securely.
                                                     (pairingKey was derived locally above —
                                                      it is NEVER sent over the wire.)
PIN cleared
```

### Length-prefixed encoding

The HMAC inputs use length-prefixed encoding to prevent canonicalization attacks: each variable-length field is encoded as a 4-byte big-endian length prefix followed by the bytes.

```text
LP(field) = 4-byte big-endian uint32 length || field bytes
```

The HMAC transcript is:

```text
LP("zaparoo-v1") || LP("p256") || LP(role) || LP(clientName) || LP(MsgA) || LP(MsgB)
```

where `role` is the literal string `"client"` or `"server"`, and `MsgA` / `MsgB` are the **raw bytes** sent over the wire at `/pair/start` (NOT the result of calling `pake.Bytes()` again after `Update()`, which would mutate the state and produce different bytes).

### Pairing limits

- **PIN**: 6 decimal digits, generated with `crypto/rand`. ~20 bits of entropy.
- **Expiry**: 5 minutes from PIN generation.
- **Attempts**: 3 failed `/pair/finish` HMAC verifications across all sessions for the same PIN before the PIN is invalidated.
- **Sessions**: A `/pair/start` session expires after 2 minutes if `/pair/finish` is not called.
- **Client name**: max 128 bytes.
- **Max paired clients**: 50 per device. The 51st pairing attempt is rejected.
- **Rate limit**: 1 request/sec per IP on `/api/pair/*` endpoints.

## Encryption — WebSocket message format

After pairing, every WebSocket connection begins with an encrypted first frame that establishes the session.

### First frame (client → server)

```json
{
    "v": 1,
    "e": "<base64(ciphertext)>",
    "t": "<authToken>",
    "s": "<base64(sessionSalt)>"
}
```

- `v`: Protocol version. Currently `1`. The server returns a plaintext error if unsupported:
  ```json
  {"jsonrpc": "2.0", "id": null, "error": {"code": -32001, "message": "unsupported encryption version", "data": {"supported": [1]}}}
  ```
- `e`: AES-256-GCM ciphertext of the JSON-RPC request, base64-encoded.
- `t`: Auth token (UUID) identifying the paired client. Sent in plaintext as a key lookup; not a secret.
- `s`: 16-byte random session salt, base64-encoded. **Must be exactly 16 bytes.** The server rejects any other size.

### Subsequent frames (both directions)

```json
{
    "e": "<base64(ciphertext)>"
}
```

The counter is implicit — both sides start at 0 and increment by 1 per frame. WebSocket is TCP (ordered, reliable) so sender and receiver always agree on the next counter value. **If decryption fails the connection is closed** — there is no recovery.

### Inside the encrypted payload

Once decrypted, the payload is standard JSON-RPC 2.0:

```json
{
    "jsonrpc": "2.0",
    "method": "version",
    "params": {},
    "id": 1
}
```

## Session key derivation

On each new WebSocket connection (NOT once per pairing), the client generates a random session salt and derives ephemeral session keys via HKDF-SHA256.

```text
prk     = HKDF-Extract(SHA-256, ikm=pairingKey, salt=sessionSalt)
c2sKey  = HKDF-Expand(SHA-256, prk, info="zaparoo-c2s-v1",       length=32)
s2cKey  = HKDF-Expand(SHA-256, prk, info="zaparoo-s2c-v1",       length=32)
c2sBase = HKDF-Expand(SHA-256, prk, info="zaparoo-c2s-nonce-v1", length=12)
s2cBase = HKDF-Expand(SHA-256, prk, info="zaparoo-s2c-nonce-v1", length=12)
```

Separate keys + nonce bases per direction prevent reflection attacks and eliminate cross-direction nonce collision.

### Counter-derived nonces

The 12-byte AES-GCM nonce for each frame is derived by XORing a 64-bit big-endian counter into the **last 8 bytes** of the 12-byte nonce base. The first 4 bytes of the base are unchanged.

```text
nonce[0:4] = base[0:4]
nonce[4:12] = base[4:12] XOR (counter as 8 bytes big-endian)
```

Counters never wrap or reuse — disconnect and reconnect with a fresh salt to start over.

## AAD (Additional Authenticated Data)

Every encrypt/decrypt operation binds the ciphertext to the auth token via AAD:

```text
aad = authToken + ":ws"
```

This prevents an attacker from substituting ciphertexts across sessions even if they somehow obtained the keys.

## Server-to-client notifications

The server broadcasts notifications (e.g. `media.started`) to all connected clients. For encrypted sessions the server encrypts each notification with the per-session keys before sending. The wire format is the same as a regular outgoing frame:

```json
{
    "e": "<base64(ciphertext)>"
}
```

## Duplicate session salt rejection

The server maintains a per-client in-memory list of recently seen session salts (sliding window of 200 entries / 10 minutes). If a client reuses a salt, the connection is rejected. This protects against broken client CSPRNGs producing duplicate salts, which would cause catastrophic AES-GCM nonce reuse.

**Always use `crypto/random` (or platform equivalent) to generate session salts.** Never derive them from time, sequence numbers, or other predictable sources.

## Failed-frame rate limiting

The server tracks consecutive failed first-frame decryptions per `(authToken, sourceIP)` pair. After 10 consecutive failures, that combination is blocked for 30 seconds (exponential backoff on repeated blocks, capped at 30 minutes). Successful first frames reset the counter.

## Client crypto dependencies

Two phases with different requirements:

**Pairing (one-time)**: Requires raw P-256 elliptic curve point arithmetic. No platform's built-in crypto API exposes this directly. Client SDKs need:

| Platform | Library |
|---|---|
| JavaScript | [`@noble/curves`](https://github.com/paulmillr/noble-curves) (audited, zero deps) |
| Python | `ecdsa` or `cryptography` |
| Swift | [Swift Crypto](https://github.com/apple/swift-crypto) or OpenSSL binding |
| Kotlin/Android | Bouncy Castle `ECPoint` (ships on Android) |
| C#/.NET | BouncyCastle NuGet |
| Rust | `p256` crate |

**Per-connection encryption (every session)**: Only needs HKDF-SHA256 + AES-256-GCM + HMAC-SHA256, all available in platform stdlib (Web Crypto, CryptoKit, JCE, .NET, RustCrypto).

## Client-side key storage

Recommend platform-appropriate secure storage:

| Platform | Recommended storage |
|---|---|
| iOS | Keychain |
| Android | EncryptedSharedPreferences / Keystore |
| Web/Electron | OS keychain via `keytar` or similar |
| CLI tools | File mode `0600` in user config directory |

The pairing key (32 bytes) and auth token (UUID) must both be stored. The auth token is not a secret but is paired with the key — losing one renders the other useless.

## Error handling

| HTTP status | Endpoint | Meaning |
|---|---|---|
| 400 | `/pair/*` | Malformed request body or PAKE message |
| 401 | `/pair/finish` | HMAC mismatch (wrong PIN) |
| 403 | `/pair/start` | Maximum paired clients reached, or attempts exhausted |
| 404 | `/pair/finish` | Unknown session ID |
| 410 | `/pair/*` | Pairing PIN expired |
| 429 | `/pair/*` | Rate limit exceeded |

WebSocket-level errors (sent as a plaintext JSON-RPC error then connection closed):

| Code | Meaning |
|---|---|
| -32001 | Unsupported encryption version |
| -32002 | Server has encryption enabled and requires an encrypted first frame from this remote client |

## Discovering server requirements

A client that does not yet know whether the server has encryption enabled should:

1. Try connecting plaintext.
2. If the server returns the `-32002` error, the client must pair before reconnecting.
3. If the user has not paired yet, prompt them to initiate pairing from the Zaparoo device's TUI.
4. Run the PAKE handshake (`/pair/start` then `/pair/finish`); locally derive `pairingKey` from the PAKE session key via `HKDF-Expand(prk, "zaparoo-pairing-v1", 32)` (the server never returns it). Store `authToken` and the derived `pairingKey` securely.
5. On all subsequent WebSocket connections, generate a fresh session salt, derive session keys, and send the encrypted first frame.

## Non-WebSocket transports

HTTP POST, SSE, and REST GET endpoints (`/api`, `/api/events`, `/r/*`, `/run/*`) are restricted to localhost by default. To allow remote access, the server operator must add IPs (or CIDR ranges) to the `allowed_ips` config field. These transports do **not** support encryption — they exist for simple DIY integrations on trusted networks. API key authentication still applies.

## Minimal JavaScript example

```javascript
// After pairing, the client has stored: authToken (string), pairingKey (Uint8Array, 32 bytes)

async function deriveSessionKeys(pairingKey, sessionSalt) {
    const ikm = await crypto.subtle.importKey("raw", pairingKey, "HKDF", false, ["deriveBits"]);
    const expand = async (info, length) => crypto.subtle.deriveBits(
        { name: "HKDF", hash: "SHA-256", salt: sessionSalt, info: new TextEncoder().encode(info) },
        ikm,
        length * 8,
    );
    return {
        c2sKey: new Uint8Array(await expand("zaparoo-c2s-v1", 32)),
        s2cKey: new Uint8Array(await expand("zaparoo-s2c-v1", 32)),
        c2sBase: new Uint8Array(await expand("zaparoo-c2s-nonce-v1", 12)),
        s2cBase: new Uint8Array(await expand("zaparoo-s2c-nonce-v1", 12)),
    };
}

function buildNonce(base, counter) {
    const nonce = new Uint8Array(12);
    nonce.set(base);
    const view = new DataView(nonce.buffer);
    const hi = Number((counter >> 32n) & 0xffffffffn);
    const lo = Number(counter & 0xffffffffn);
    view.setUint32(4, view.getUint32(4) ^ hi, false);
    view.setUint32(8, view.getUint32(8) ^ lo, false);
    return nonce;
}

async function connectEncrypted(wsUrl, authToken, pairingKey) {
    const sessionSalt = crypto.getRandomValues(new Uint8Array(16));
    const keys = await deriveSessionKeys(pairingKey, sessionSalt);
    const c2sKey = await crypto.subtle.importKey("raw", keys.c2sKey, "AES-GCM", false, ["encrypt"]);
    const s2cKey = await crypto.subtle.importKey("raw", keys.s2cKey, "AES-GCM", false, ["decrypt"]);
    const aad = new TextEncoder().encode(authToken + ":ws");

    let sendCounter = 0n;
    let recvCounter = 0n;

    const ws = new WebSocket(wsUrl);

    const opened = new Promise((resolve, reject) => {
        ws.onopen = () => resolve();
        ws.onerror = (e) => reject(e);
    });

    async function sendRPC(method, params) {
        await opened;
        const payload = JSON.stringify({ jsonrpc: "2.0", method, params, id: Date.now() });
        const counter = sendCounter++;
        const nonce = buildNonce(keys.c2sBase, counter);
        const ct = await crypto.subtle.encrypt(
            { name: "AES-GCM", iv: nonce, additionalData: aad },
            c2sKey,
            new TextEncoder().encode(payload),
        );
        const ctB64 = btoa(String.fromCharCode(...new Uint8Array(ct)));
        const msg = { e: ctB64 };
        if (counter === 0n) {
            msg.v = 1;
            msg.t = authToken;
            msg.s = btoa(String.fromCharCode(...sessionSalt));
        }
        ws.send(JSON.stringify(msg));
    }

    ws.onmessage = async (ev) => {
        const frame = JSON.parse(ev.data);
        if (!frame.e) return;
        const ct = Uint8Array.from(atob(frame.e), (c) => c.charCodeAt(0));
        const counter = recvCounter++;
        const nonce = buildNonce(keys.s2cBase, counter);
        const pt = await crypto.subtle.decrypt(
            { name: "AES-GCM", iv: nonce, additionalData: aad },
            s2cKey,
            ct,
        );
        handleResponse(JSON.parse(new TextDecoder().decode(pt)));
    };

    await opened;
    return { sendRPC };
}
```

This is a minimal working example. Production clients should add reconnection logic, error handling for HMAC mismatches, and secure storage for the pairing key.
