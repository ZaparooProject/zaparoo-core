# Notifications

Notifications are sent from the server to connected clients to inform them of events.

## Launching

### running

Media started running on the server.

## Readers

### readers.added

A new reader was connected to the server.

#### Response

| Key       | Type    | Required | Description                          |
| :-------- | :------ | :------- | :----------------------------------- |
| connected | boolean | Yes      | Whether the reader is connected.     |
| driver    | string  | Yes      | Driver type for the reader.          |
| path      | string  | Yes      | System path or identifier of reader. |

### readers.removed

A connected reader was disconnected from the server.

#### Response

| Key       | Type    | Required | Description                          |
| :-------- | :------ | :------- | :----------------------------------- |
| connected | boolean | Yes      | Whether the reader is connected.     |
| driver    | string  | Yes      | Driver type for the reader.          |
| path      | string  | Yes      | System path or identifier of reader. |

## Tokens

### tokens.added

A token was detected by a connected reader.

#### Response

| Key      | Type   | Required | Description                                    |
| :------- | :----- | :------- | :--------------------------------------------- |
| type     | string | Yes      | Type of token (e.g., "nfc", "barcode").        |
| uid      | string | Yes      | Unique identifier of the token.                |
| text     | string | No       | Text data associated with the token.           |
| data     | string | No       | Raw binary data of the token (base64 encoded). |
| scanTime | string | Yes      | ISO 8601 timestamp when token was scanned.     |

### tokens.removed

A token was removed from a connected reader.

#### Response

Returns `null`.

## Media

### media.started

New media was started on server.

#### Response

| Key        | Type   | Required | Description                                  |
|:-----------|:-------|:---------|:---------------------------------------------|
| systemId   | string | Yes      | Internal ID of system associated with media. |
| systemName | string | Yes      | Display name of system.                      |
| mediaPath  | string | Yes      | Path to media file on server.                |
| mediaName  | string | Yes      | Cleaned display title of media.              |

#### Example

```json
{
  "systemId": "SNES",
  "systemName": "Super Nintendo Entertainment System",
  "mediaPath": "/roms/snes/Super_Mario_World_(USA)_[!].sfc",
  "mediaName": "Super Mario World"
}
```

### media.stopped

Media has stopped on server.

#### Response

Returns `null`.

### media.indexing

Sent during media database generation to indicate indexing progress and completion status, as well as database optimization progress.

**Priority:** Indexing takes priority over optimization in notifications. If both are running, only indexing status will be shown. Optimization status is shown when no indexing is in progress.

#### Parameters

| Key                | Type    | Required | Description                                                                                      |
| :----------------- | :------ | :------- | :----------------------------------------------------------------------------------------------- |
| exists             | boolean | Yes      | True if media database exists.                                                                   |
| indexing           | boolean | Yes      | True if indexing is currently in progress.                                                       |
| optimizing         | boolean | Yes      | True if database optimization is currently in progress.                                          |
| totalSteps         | number  | No       | Total number of systems to process during indexing.                                              |
| currentStep        | number  | No       | Current system being processed during indexing (1-based).                                        |
| currentStepDisplay | string  | No       | Display name of current system being indexed, or optimization step name (e.g., `"vacuum"`).      |
| totalFiles         | number  | No       | Total number of media files discovered during indexing.                                          |
| totalMedia         | number  | No       | Total number of media entries in the database. Only included when database exists and is ready.  |

**Indexing Progress:** Track using `currentStep` out of `totalSteps` systems processed.

**Optimization Progress:** When `optimizing` is true and `indexing` is false, `currentStepDisplay` shows the optimization operation name (e.g., `"vacuum"`, `"analyze"`).

#### Examples

##### Indexing In Progress

```json
{
  "jsonrpc": "2.0",
  "method": "media.indexing",
  "params": {
    "exists": true,
    "indexing": true,
    "optimizing": false,
    "totalSteps": 50,
    "currentStep": 25,
    "currentStepDisplay": "SNES",
    "totalFiles": 1523
  }
}
```

##### Optimization In Progress

```json
{
  "jsonrpc": "2.0",
  "method": "media.indexing",
  "params": {
    "exists": true,
    "indexing": false,
    "optimizing": true,
    "currentStepDisplay": "vacuum",
    "totalMedia": 5432
  }
}
```

##### Database Ready

```json
{
  "jsonrpc": "2.0",
  "method": "media.indexing",
  "params": {
    "exists": true,
    "indexing": false,
    "optimizing": false,
    "totalMedia": 5432
  }
}
```

## Playtime

### playtime.limit.reached

Sent when a playtime limit (session or daily) has been reached and enforced by the system. The currently playing game will be stopped when this notification is sent.

#### Parameters

| Key    | Type   | Required | Description                                                       |
| :----- | :----- | :------- | :---------------------------------------------------------------- |
| reason | string | Yes      | The type of limit that was reached: `"session"` or `"daily"`.     |

#### Examples

##### Session Limit Reached

```json
{
  "jsonrpc": "2.0",
  "method": "playtime.limit.reached",
  "params": {
    "reason": "session"
  }
}
```

##### Daily Limit Reached

```json
{
  "jsonrpc": "2.0",
  "method": "playtime.limit.reached",
  "params": {
    "reason": "daily"
  }
}
```

### playtime.limit.warning

Sent at configured time intervals to warn that a playtime limit is approaching. These notifications are sent based on the `warnings` configuration (e.g., at 5 minutes, 2 minutes, and 1 minute before limit).

The warning applies to whichever limit will be reached first (session or daily).

#### Parameters

| Key       | Type   | Required | Description                                                                               |
| :-------- | :----- | :------- | :---------------------------------------------------------------------------------------- |
| interval  | string | Yes      | The configured warning interval that triggered this notification (Go duration format).     |
| remaining | string | Yes      | The actual time remaining before the limit is reached (Go duration format).               |

**Note:** `remaining` may differ slightly from `interval` due to timing precision.

#### Example

```json
{
  "jsonrpc": "2.0",
  "method": "playtime.limit.warning",
  "params": {
    "interval": "5m",
    "remaining": "4m58s"
  }
}
```
