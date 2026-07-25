# Notifications

Notifications are sent from the server to connected clients to inform them of events.

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
| readerId | string | No       | ID of the reader that scanned the token.       |

### tokens.staged

A token was staged by the launch guard and is waiting for confirmation. Sent when launch guard is enabled and media is currently playing.

#### Response

| Key      | Type   | Required | Description                                    |
| :------- | :----- | :------- | :--------------------------------------------- |
| type     | string | Yes      | Type of token (e.g., "nfc", "barcode").        |
| uid      | string | Yes      | Unique identifier of the token.                |
| text     | string | No       | Text data associated with the token.           |
| data     | string | No       | Raw binary data of the token (base64 encoded). |
| scanTime | string | Yes      | ISO 8601 timestamp when token was scanned.     |

### tokens.staged.ready

A staged token's delay period has expired and is now ready for confirmation. Sent when launch guard delay is configured and the mandatory waiting period completes.

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

## UI

### ui.changed

Authoritative global UI state changed. Sent when event opens, updates, is replaced, or resolves. Clients should replace local event snapshot with `events` from newest revision rather than applying incremental patches.

Host platform and all connected clients may render same event in parallel. First valid response wins. A terminal update removes event from `events` and describes it in `resolved`, instructing every renderer to close.

`ui.changed` is latest-wins coalescible. Clients that reconnect or suspect missed notifications should query [`ui`](./methods.md#ui).

#### Parameters

| Key      | Type   | Required | Description |
| :------- | :----- | :------- | :---------- |
| revision | number | Yes      | Monotonic revision of process-wide UI state shared across clients. Ignore older values. |
| events   | object[] | Yes    | Complete active event snapshot. Initial implementation contains zero or one event. |
| resolved | object[] | Yes    | Terminal resolutions associated with this transition. |

Event and resolution fields are defined under [`ui`](./methods.md#ui-event-object) and [`ui.respond`](./methods.md#ui-resolution-object).

#### Picker opened

```json
{
  "jsonrpc": "2.0",
  "method": "ui.changed",
  "params": {
    "revision": 12,
    "events": [
      {
        "id": "68bb6f25-dbd4-46a0-a3ac-e4fb54928ac2",
        "kind": "picker",
        "title": "Favorites",
        "choices": [
          {"id": "e88560a7-76bb-4ab4-a914-da4be745778d", "label": "Game One"},
          {"id": "ce99379a-b803-4489-9da4-e752e96471e9", "label": "Game Two"}
        ],
        "selectedChoiceId": "e88560a7-76bb-4ab4-a914-da4be745778d",
        "dismissible": true,
        "createdAt": "2026-07-16T12:00:00Z",
        "expiresAt": "2026-07-16T12:00:30Z"
      }
    ],
    "resolved": []
  }
}
```

#### Picker resolved

```json
{
  "jsonrpc": "2.0",
  "method": "ui.changed",
  "params": {
    "revision": 13,
    "events": [],
    "resolved": [
      {
        "id": "68bb6f25-dbd4-46a0-a3ac-e4fb54928ac2",
        "outcome": "selected",
        "choiceId": "ce99379a-b803-4489-9da4-e752e96471e9"
      }
    ]
  }
}
```

Launch guard continues emitting `tokens.staged` and `tokens.staged.ready` for compatibility while also opening a global `confirm` UI event for API clients. Core does not present this specific confirmation through host platform renderer, preserving existing sound and card re-tap behavior during active media.

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

| Key        | Type   | Required | Description                                    |
| :--------- | :----- | :------- | :--------------------------------------------- |
| systemId   | string | Yes      | ID of the system.                              |
| systemName | string | Yes      | Display name of the system.                    |
| mediaName  | string | Yes      | Display name of the media.                     |
| mediaPath  | string | Yes      | Path to media file on server.                  |
| launcherId | string | Yes      | ID of the launcher.                            |
| elapsed    | number | Yes      | Duration of the media session in seconds.      |

#### Example

```json
{
  "jsonrpc": "2.0",
  "method": "media.stopped",
  "params": {
    "systemId": "SNES",
    "systemName": "Super Nintendo Entertainment System",
    "mediaName": "Super Mario World",
    "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
    "launcherId": "SNES",
    "elapsed": 2730
  }
}
```

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

##### Indexing in progress

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

##### Optimization in progress

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

##### Database ready

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

### media.scraping

Sent while a metadata scraper run is active and when it completes.

The first notification for a scraper run identifies the scraper and sets `scraping` to true. Progress notifications include the current system, per-system counters, whole-run system-step progress, pause state, and completion state. A final notification has `scraping` set to false and `done` set to true. Existing flat counter fields remain for compatibility; new UIs should prefer `currentSystem` for per-system progress and `totalSteps`/`currentStep`/`currentStepDisplay` for whole-run progress.

#### Parameters

| Key       | Type    | Required | Description                                                       |
| :-------- | :------ | :------- | :---------------------------------------------------------------- |
| scraperId | string  | No       | Scraper ID, for example `gamelist.xml`.                           |
| systemId  | string  | No       | System currently being scraped.                                   |
| processed | number  | Yes      | Number of source records processed so far.                        |
| total     | number  | Yes      | Total source records for the current system, or 0 before known.   |
| matched   | number  | Yes      | Number of records matched to existing media rows.                 |
| skipped   | number  | Yes      | Number of records skipped because they were unmatched, already scraped, or failed per-record processing. |
| totalScraped | number | Yes   | Number of media records already marked scraped.                   |
| scraping  | boolean | Yes      | True while scraping is active.                                    |
| done      | boolean | Yes      | True on the terminal update for the scraper run.                  |
| paused    | boolean | Yes      | True when the active scrape is paused.                            |
| state     | string  | No       | Explicit lifecycle state: `idle`, `running`, `paused`, `completed`, `cancelled`, or `failed`. |
| error     | string  | No       | Fatal scrape error on failed terminal updates.                    |
| totalSteps | number | No       | Total systems in the scrape run, when known.                      |
| currentStep | number | No      | 1-based current system step, when known.                          |
| currentStepDisplay | string | No | Display name for the current system step, falling back to system ID. |
| currentSystem | object | No    | Per-system progress object with `systemId`, `systemName`, `processed`, `total`, `matched`, and `skipped`. |

#### Examples

##### Scraping in progress

```json
{
  "jsonrpc": "2.0",
  "method": "media.scraping",
  "params": {
    "scraperId": "gamelist.xml",
    "systemId": "SNES",
    "processed": 42,
    "total": 100,
    "matched": 38,
    "skipped": 4,
    "totalScraped": 1200,
    "scraping": true,
    "done": false,
    "paused": false,
    "state": "running",
    "totalSteps": 12,
    "currentStep": 3,
    "currentStepDisplay": "Super Nintendo Entertainment System",
    "currentSystem": {
      "systemId": "SNES",
      "systemName": "Super Nintendo Entertainment System",
      "processed": 42,
      "total": 100,
      "matched": 38,
      "skipped": 4
    }
  }
}
```

##### Scraping complete

```json
{
  "jsonrpc": "2.0",
  "method": "media.scraping",
  "params": {
    "scraperId": "gamelist.xml",
    "systemId": "SNES",
    "processed": 100,
    "total": 100,
    "matched": 92,
    "skipped": 8,
    "totalScraped": 1250,
    "scraping": false,
    "done": true,
    "paused": false,
    "state": "completed",
    "totalSteps": 12,
    "currentStep": 12,
    "currentStepDisplay": "Super Nintendo Entertainment System",
    "currentSystem": {
      "systemId": "SNES",
      "systemName": "Super Nintendo Entertainment System",
      "processed": 100,
      "total": 100,
      "matched": 92,
      "skipped": 8
    }
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

##### Session limit reached

```json
{
  "jsonrpc": "2.0",
  "method": "playtime.limit.reached",
  "params": {
    "reason": "session"
  }
}
```

##### Daily limit reached

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

## Inbox

### inbox.added

Sent when a new inbox message is added to the server.

#### Parameters

| Key       | Type   | Required | Description                                      |
| :-------- | :----- | :------- | :----------------------------------------------- |
| id        | number | Yes      | Unique identifier of the message.                |
| title     | string | Yes      | Title of the message.                            |
| body      | string | No       | Body text of the message.                        |
| severity  | number | Yes      | Severity level (0=info, 1=warning, 2=error).     |
| category  | string | No       | Category of the message.                         |
| profileId | number | No       | Associated profile ID, if applicable.            |
| createdAt | string | Yes      | Timestamp when message was created in RFC3339 format. |

#### Example

```json
{
  "jsonrpc": "2.0",
  "method": "inbox.added",
  "params": {
    "id": 1,
    "title": "Update Available",
    "body": "A new version of Zaparoo is available.",
    "severity": 0,
    "category": "update",
    "createdAt": "2024-09-24T17:49:42.938167429+08:00"
  }
}
```

## Profiles

### profiles.active

Sent when the device's active profile changes, including deactivation.

#### Parameters

| Key     | Type   | Required | Description                                                        |
| :------ | :----- | :------- | :------------------------------------------------------------------ |
| profile | object \| null | Yes | The new active profile, or null when the device deactivated.       |

The profile object contains `profileId`, `name`, `hasPin` and any playtime limit overrides (`limitsEnabled`, `dailyLimit`, `sessionLimit`).

#### Example

```json
{
  "jsonrpc": "2.0",
  "method": "profiles.active",
  "params": {
    "profile": {
      "profileId": "1ad28b9a-7aef-11ef-9817-020304050607",
      "name": "Kid A",
      "hasPin": true,
      "limitsEnabled": true,
      "dailyLimit": "2h"
    }
  }
}
```

### profiles.data

Sent when a profile data swap (save files, save states) changes state on
platforms that support data swapping. The profile switch itself always
succeeds independently of data swapping; this notification reports the
data side.

#### Parameters

| Key       | Type   | Required | Description                                                          |
| :-------- | :----- | :------- | :-------------------------------------------------------------------- |
| profileId | string | Yes      | Profile whose data the swap targets. Empty for the shared profile.   |
| status    | string | Yes      | One of `applied`, `deferred` (media is running; applies when it stops), `failed`, or `unavailable` (the storage setup does not permit swapping, e.g. a read-only network share). |
| reason    | string | No       | Human-readable explanation for `failed`/`unavailable`.               |

#### Example

```json
{
  "jsonrpc": "2.0",
  "method": "profiles.data",
  "params": {
    "profileId": "1ad28b9a-7aef-11ef-9817-020304050607",
    "status": "deferred"
  }
}
```

## Auth

### auth.link.status

Sent on every state transition of a device link flow started with `settings.auth.link`. Notification payloads always omit the user code and verification URLs; clients that need them read the `settings.auth.link` result or poll `settings.auth.link.status`.

#### Parameters

| Key       | Type   | Required | Description                                                     |
| :-------- | :----- | :------- | :-------------------------------------------------------------- |
| status    | string | Yes      | One of `pending`, `approved`, `failed`, or `cancelled`.         |
| expiresAt | string | No       | RFC 3339 time when the link request expires.                    |
| error     | string | No       | Human-readable reason when `status` is `failed`.                |

#### Example

```json
{
  "jsonrpc": "2.0",
  "method": "auth.link.status",
  "params": {
    "status": "approved"
  }
}
```

## Backup

### backup.state

Sent while a backup operation (local create, cloud upload, or restore) is running, whenever its pause/throttle state changes because a game started or stopped, and once with `finished` true when the operation ends. Backup work follows the same policy as media indexing: most games throttle it, storage-sensitive CD-based cores pause it entirely, and it resumes when the game stops. A notification with `paused`, `throttled`, and `finished` all false means the operation returned to full speed.

The `finished` notification is terminal for that operation, whatever its outcome — use it to clear any paused/slowed indicator, and read `settings.backup.status` for the result.

#### Parameters

| Key       | Type    | Required | Description                                                                              |
| :-------- | :------ | :------- | :--------------------------------------------------------------------------------------- |
| operation | string  | No       | The active operation kind, matching `activeOperation` from `settings.backup.status`.     |
| paused    | boolean | Yes      | True if the operation is fully paused until the running game stops.                      |
| throttled | boolean | Yes      | True if the operation is running slowed to stay out of the running game's way.           |
| finished  | boolean | No       | True when the operation has ended; no further `backup.state` events follow for it.       |

#### Examples

##### Upload paused by a running game

```json
{
  "jsonrpc": "2.0",
  "method": "backup.state",
  "params": {
    "operation": "remote-upload",
    "paused": true,
    "throttled": false
  }
}
```

##### Operation finished

```json
{
  "jsonrpc": "2.0",
  "method": "backup.state",
  "params": {
    "operation": "remote-upload",
    "paused": false,
    "throttled": false,
    "finished": true
  }
}
```
