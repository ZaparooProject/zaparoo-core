# Core API

The **Core API** is stable and ready for third-party integrations. The published `/api/v0.1` contract will receive backward-compatible, additive changes only. Any future breaking changes will use a new versioned endpoint.

The **Core API** is available on and published by every device running the [Zaparoo Core](../../core) software. This API allows management of all Zaparoo features locally and remotely. The [Zaparoo](https://zaparoo.app/) app uses this API for all communication with Zaparoo devices, as do most of the flags when Zaparoo is run via the command line.

This page documents the protocol used to communicate with the API and how to interact with it. _It is currently the source of truth when developing applications that work with Zaparoo._

## Communication Protocol

The API uses a standard [WebSocket](https://developer.mozilla.org/en-US/docs/Web/API/WebSocket) connection to exchange [JSON](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/JSON) payloads using the [JSON-RPC 2.0](https://www.jsonrpc.org/specification) protocol.

Communication follows a loose client-server relationship. Clients, by default, are not expected to implement the API beyond what that particular client needs to function.

### Connection

Connections to the API can be established with any standard WebSocket client, using the versioned endpoint of the HTTP server published by the Zaparoo service. By default, the HTTP server is accessible on port `7497`. This port is configurable by the user.

These endpoints are currently available:

- `/api/v0.1`
- `/api/v0`
- `/api/`

It's highly recommended to target your application at the specific minor version of the endpoint you're using, which will maintain compatibility during API updates.

An example address for connecting to the API: `ws://10.0.0.123:7497/api/v0.1`

The connection requires no special configuration or authentication to initiate. When [encryption](./encryption) is enabled, remote clients must complete a one-time pairing and encrypt all WebSocket traffic.

### HTTP POST

All API methods can also be called via HTTP POST requests to the same versioned endpoints used for WebSocket connections. This provides an alternative for applications that prefer REST-style communication over persistent WebSocket connections.

These HTTP endpoints are available:

- `/api/v0.1`
- `/api/v0`
- `/api/`

HTTP requests must use the `POST` method with `Content-Type: application/json` and send the same JSON-RPC 2.0 formatted payloads as WebSocket connections. The server will respond with a single JSON-RPC 2.0 response object.

An example HTTP request to get the server version:

```bash
curl -X POST http://10.0.0.123:7497/api/v0.1 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "method": "version"
  }'
```

This would return a response like:

```json
{
  "jsonrpc": "2.0",
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "result": {
    "version": "0.7.0",
    "platform": "linux"
  }
}
```

Unlike WebSocket connections, HTTP requests are stateless and do not support notifications. Each request requires a complete JSON-RPC 2.0 payload and will receive a single response.

### Server-Sent Events (SSE)

The API provides a [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) endpoint for receiving notifications over a standard HTTP connection. This is useful for clients that only need to receive notifications without the full bidirectional communication of WebSocket, and works with any HTTP client without additional libraries.

These SSE endpoints are available:

- `/api/v0.1/events`
- `/api/v0/events`
- `/api/events`

An example using `curl`:

```bash
curl -N http://10.0.0.123:7497/api/v0.1/events
```

Each notification is sent as an SSE `data` field containing the same JSON-RPC 2.0 notification object used by WebSocket:

```
data: {"jsonrpc":"2.0","method":"tokens.added","params":{"type":"nfc","uid":"04E1234567890","text":"**launch:game.rom"}}

data: {"jsonrpc":"2.0","method":"media.started","params":{"systemId":"NES","systemName":"NES","name":"game.rom"}}
```

SSE connections are long-lived and will continue receiving events until the client disconnects. To call methods, use HTTP POST to the standard API endpoint alongside the SSE connection.

### JSON Payloads

Server and clients communicate back and forth using JSON payloads, following the [JSON-RPC 2.0](https://www.jsonrpc.org/specification) protocol.

Because a WebSocket connection is asynchronous, request payloads are tagged with a unique ID. The client must keep track of IDs sent to another client and wait for a matching response object. A client can continue sending requests while waiting for responses to previous requests.

#### Requests

A request object asks the connected server to run a predefined [method](#methods), and report back when it's completed with a response object.

An example request:

```json
{
  "jsonrpc": "2.0",
  "id": "4b5da056-a5d4-436b-b4e6-b96231e99969",
  "method": "media.search",
  "params": {
    "query": "240p"
  }
}
```

This request would query Zaparoo's media database for a filename containing the word "240p" and return a response with the search results.

##### Request object

| Key     | Type   | Required | Description                                                                                                                                                                                   |
| :------ | :----- | :------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| jsonrpc | string | Yes      | As per the JSON-RPC 2.0 spec, this key's value must be the string `2.0` for a payload to be accepted.                                                                                         |
| id      | string | Yes\*    | A UUID generated by the requesting client, used to match requests back to responses. A request missing this key is valid but would be treated as a notification and not receive any response. |
| method  | string | Yes      | A string corresponding to a method to be run by the receiving server.                                                                                                                         |
| params  | any    | No       | Arguments supplied for the method. The value of this key depends on the method used and is omitted for some methods.                                                                          |

All available request methods and their parameters are [documented below](#methods).

#### Notifications

Notifications are requests which do not contain an ID. Otherwise, they are identical to a standard request object. Notifications can be sent by either server or client and do not receive a response.

Like standard requests, notifications may or may not have parameters and its value will depend on the method. Types of notifications are [documented below](#notifications).

#### Responses

Every request sent must have a matching response. An example response to the `media.search` request shown above:

```json
{
  "jsonrpc": "2.0",
  "id": "4b5da056-a5d4-436b-b4e6-b96231e99969",
  "result": {
    "results": [
      {
        "system": {
          "id": "Gameboy",
          "name": "Gameboy"
        },
        "name": "240p Test Suite (PD) v0.03 tepples",
        "path": "Gameboy/240p Test Suite (PD) v0.03 tepples.gb",
        "zapScript": "@Gameboy/240p Test Suite (PD) v0.03 tepples",
        "tags": []
      }
    ],
    "total": 1,
    "pagination": {
      "hasNextPage": false,
      "pageSize": 100
    }
  }
}
```

##### Response object

| Key     | Type   | Required | Description                                                                                                                                                     |
| :------ | :----- | :------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| jsonrpc | string | Yes      | Same as a [request](#requests).                                                                                                                                 |
| id      | string | Yes      | Same as a [request](#requests). The same ID sent by the original request.                                                                                       |
| result  | any    | No\*     | Return value of the method. May be `null` depending on the method, will be missing if there was an error. See [methods](#methods) for possible values.          |
| error   | Error  | No\*     | If a method failed, this key will be populated with the error details and the result key will be empty. See [below](#response-errors) for details about errors. |

##### Response errors

If a method fails, it will populate the `error` key in the response object with details about the failure. An example of a failed request:

```json
{
  "jsonrpc": "2.0",
  "id": "4b5da056-a5d4-436b-b4e6-b96231e99969",
  "error": {
    "code": 1,
    "message": "invalid cursor: invalid base64"
  }
}
```

##### Error object

| Key     | Type   | Required | Description                                                                               |
| :------ | :----- | :------- | :---------------------------------------------------------------------------------------- |
| code    | number | Yes      | An integer specifying the general error category. **Error codes are not yet formalised.** |
| message | string | Yes      | Short human readable message explaining the error cause, if possible.                     |

#### Protocol Errors

If a low-level error occurs before a request context can be established, a protocol error will be sent back. This can happen, for example, if a JSON payload is malformed or a payload could not be decrypted. They're identical to an error response except they will have no ID.

Protocol errors may be sent cleartext if a secure context couldn't be established, but will not contain any sensitive data.

### Anonymous Access

Anonymous cleartext access is, generally, allowed when an API connection is made from a loopback address (i.e. from the same device Zaparoo is running). This access depends on the platform and whether the service is running with elevated privileges. Check the page for the specific platform you're using to make sure it's available to you.

This access is also allowed when a connection is made over a WebSocket Secure (wss) connection.

### Permissions

Core evaluates permissions for each request from connection locality and paired-client role:

- **Localhost** requests originate from Core's device and have full access.
- Paired **admin** clients have the `profiles.manage`, `settings.write`, and `update.apply` capabilities.
- Paired **member** clients can use day-to-day methods, but cannot manage profiles, change protected settings, or apply updates.
- **Unpaired remote** clients are possible only when encryption is disabled. For backward compatibility they receive admin capabilities, but methods explicitly restricted to localhost or to a paired admin still reject them. They do not get the `update.apply` capability, and `update.check` is closed to them too.

Call [`clients.current`](./methods.md#clientscurrent) to inspect current connection's role and capabilities. Every method in [API Methods](./methods) states its access requirements. Some read methods return additional sensitive fields to privileged clients; those fields are identified in their result contracts.

### Heartbeat

If sent the bytes `ping`, the API will immediately respond with the bytes `pong`. This feature can be used by heartbeat functions in WebSocket libraries.

## Launch Endpoint

The HTTP server has additional endpoints which allow restricted access to launch ZapScript using a GET request. These endpoints are specifically meant to support uses such as QR codes scanned by a phone's camera app or simple launch testing.

The following endpoints are available:

- `/run/` - Preferred endpoint for launching ZapScript.
- `/r/` - Alias for `/run/`.
- `/l/` - **Deprecated.** Use `/run/` instead.

An example request: `GET http://10.0.0.123:7497/run/**launch.system:snes`

This would act as though a token with the text `**launch.system:snes` had been scanned.

URL-encode media paths used in the endpoint. Core decodes the path once before running it, including encoded spaces and parentheses:

```http
GET http://10.0.0.123:7497/run/_Arcade/Youjyuden%20%28JP%29.mra
```

This runs `_Arcade/Youjyuden (JP).mra`.

Requests from the local device are allowed without restriction. Remote requests must be explicitly allowed using the `allow_run` config setting.

## Methods

Methods execute actions and return data from Core. This catalog documents **88 non-deprecated registered methods**. See [API Methods](./methods) for request and response contracts, complete access details, and examples. **Local/admin** means localhost or paired admin; **Tiered** means fields or availability vary by client and are detailed in method reference.

| ID                              | Description                                                                           | Access |
| :------------------------------ | :------------------------------------------------------------------------------------ | :----- |
| run                             | Run supplied ZapScript.                                                               | All clients |
| stop                            | Kill any active launcher, if possible.                                                | All clients |
| confirm                         | Confirm and launch the currently staged token.                                        | All clients |
| ui                              | Return authoritative global UI event state.                                           | All clients |
| ui.respond                      | Respond to active global UI event.                                                     | All clients |
| tokens                          | List active tokens.                                                                   | All clients |
| tokens.history                  | Return latest token launches.                                                         | All clients |
| media                           | Return media database status and active media.                                        | All clients |
| media.generate                  | Start a media database index.                                                         | All clients |
| media.generate.cancel           | Cancel active media database indexing.                                                | All clients |
| media.generate.resume           | Resume paused media database indexing.                                                | All clients |
| media.search                    | Search indexed media.                                                                 | All clients |
| media.browse                    | Browse indexed media in a directory hierarchy.                                        | All clients |
| media.browse.index              | Return jump-to-letter buckets and seek cursors for browsing.                          | All clients |
| media.tags                      | Query tags available for media filtering.                                             | All clients |
| media.tags.update               | Add or remove user tags for indexed media.                                            | All clients |
| media.meta.update               | Update writable metadata for indexed media.                                           | All clients |
| media.active                    | Return currently active media.                                                        | All clients |
| media.active.update             | Update currently active media information.                                            | All clients |
| media.clean.orphans             | Remove orphaned media database rows.                                                  | All clients |
| media.history                   | Return paginated media play history.                                                  | All clients |
| media.history.latest            | Return latest media play history entries.                                             | All clients |
| media.history.top               | Return most-played media ranked by play time.                                         | All clients |
| media.lookup                    | Resolve game name and system to indexed media.                                        | All clients |
| media.meta                      | Return metadata for indexed media.                                                    | All clients |
| media.image                     | Return best matching image for indexed media.                                         | All clients |
| scrapers                        | List available metadata scrapers.                                                     | All clients |
| media.scrape                    | Start metadata scraping.                                                              | All clients |
| media.scrape.status             | Return latest metadata scraping status.                                               | All clients |
| media.scrape.cancel             | Cancel active metadata scraping.                                                      | All clients |
| media.scrape.resume             | Resume paused metadata scraping.                                                      | All clients |
| media.control                   | Send control action to active media launcher.                                         | All clients |
| media.title.parse               | Preview media title and slug parsing.                                                  | All clients |
| settings                        | List current configuration settings.                                                  | Tiered |
| settings.update                 | Update and save configuration settings.                                               | `settings.write` |
| settings.reload                 | Reload settings from disk.                                                            | All clients |
| settings.logs.download          | Download current log file.                                                            | All clients |
| settings.backup                 | Create local device backup.                                                           | Local/admin |
| settings.backup.list            | List local device backups.                                                            | Local/admin |
| settings.backup.inspect         | Inspect local device backup.                                                          | Local/admin |
| settings.backup.delete          | Delete local device backup.                                                           | Local/admin |
| settings.backup.restore         | Restore local device backup.                                                          | Local/admin |
| settings.backup.status          | Return local and remote backup status.                                                | All clients |
| settings.backup.remote.run      | Create remote device backup.                                                          | Local/admin |
| settings.backup.remote.list     | List remote device backups.                                                           | Local/admin |
| settings.backup.remote.restore  | Restore remote device backup.                                                         | Local/admin |
| settings.playtime.limits        | Return playtime limit configuration.                                                  | All clients |
| settings.playtime.limits.update | Update playtime limits.                                                               | `settings.write` |
| playtime                        | Return playtime session status and usage.                                             | All clients |
| systems                         | List indexed or supported systems.                                                    | All clients |
| launchers                       | List launchers known to running service.                                              | All clients |
| launchers.refresh               | Refresh launcher cache.                                                               | All clients |
| mappings                        | List mappings.                                                                        | All clients |
| mappings.new                    | Create mapping.                                                                       | All clients |
| mappings.delete                 | Delete mapping.                                                                       | All clients |
| mappings.update                 | Update mapping.                                                                       | All clients |
| mappings.reload                 | Reload mappings from disk.                                                            | All clients |
| readers                         | List connected readers and capabilities.                                              | All clients |
| readers.write                   | Write text using available write-capable reader.                                      | All clients |
| readers.write.cancel            | Cancel active reader write.                                                           | All clients |
| input.keyboard                  | Send keyboard input sequence.                                                         | All clients |
| input.gamepad                   | Send gamepad input sequence.                                                          | All clients |
| screenshot                      | Capture platform display.                                                             | All clients |
| version                         | Return Core version and platform.                                                     | All clients |
| health                          | Check whether Core is responding.                                                     | All clients |
| inbox                           | List inbox messages.                                                                  | All clients |
| inbox.delete                    | Delete inbox message.                                                                 | All clients |
| inbox.clear                     | Delete all inbox messages.                                                            | All clients |
| clients                         | List paired clients.                                                                  | Localhost |
| clients.current                 | Return current connection role and capabilities.                                      | All clients |
| clients.delete                  | Revoke paired client.                                                                 | Localhost |
| clients.pair.start              | Start pairing flow.                                                                   | Localhost |
| clients.pair.cancel             | Cancel pairing flow.                                                                  | Localhost |
| profiles                        | List profiles.                                                                        | Tiered |
| profiles.new                    | Create profile.                                                                       | `profiles.manage` |
| profiles.update                 | Update profile.                                                                       | `profiles.manage` |
| profiles.delete                 | Delete profile.                                                                       | `profiles.manage` |
| profiles.active                 | Return active profile.                                                                | All clients |
| profiles.switch                 | Switch active profile.                                                                | All clients |
| profiles.verify                 | Verify profile PIN.                                                                   | All clients |
| settings.auth.claim             | Claim API credentials from remote server.                                             | All clients |
| settings.auth.status            | Return credential link status.                                                        | All clients |
| settings.auth.unlink            | Remove online account credentials.                                                    | Local/admin |
| settings.auth.link              | Start online account link.                                                            | Local/admin |
| settings.auth.link.status       | Return online account link flow status.                                               | Tiered |
| settings.auth.link.cancel       | Cancel online account link flow.                                                      | Local/admin |
| update.check                    | Check for newer Core version.                                                         | Localhost or any paired client |
| update.apply                    | Apply latest update and restart gracefully.                                           | `update.apply` |

## Notifications

Notifications let a server or client know an event has occurred. See the [API Notifications](./notifications) page for detailed definitions and examples of each notification.

| ID                     | Description                                                                       |
| :--------------------- | :-------------------------------------------------------------------------------- |
| readers.added          | A new reader was connected to the server.                                         |
| readers.removed        | A connected reader was disconnected from the server.                              |
| tokens.added           | A new token detected by a reader.                                                 |
| tokens.removed         | A token was removed.                                                              |
| tokens.staged          | A token was staged by launch guard and is awaiting confirmation.                  |
| tokens.staged.ready    | A staged token's delay period has expired and is ready for confirmation.          |
| ui.changed             | Authoritative global UI event state changed.                                      |
| media.started          | New media was started on server.                                                  |
| media.stopped          | Media has stopped on server.                                                      |
| media.indexing         | The state of the indexing or optimization process has changed.                    |
| media.scraping         | Progress updates emitted during media scraping (includes progress/status details). |
| playtime.limit.reached | A playtime limit (session or daily) has been reached and enforced.                |
| playtime.limit.warning | A playtime warning notification sent at configured intervals before limit reached. |
| inbox.added            | A new inbox message was added to the server.                                      |
| update.state           | Progress of an update being applied.                                              |
