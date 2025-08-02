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
| :--------- | :----- | :------- | :------------------------------------------- |
| systemId   | string | Yes      | Internal ID of system associated with media. |
| systemName | string | Yes      | Display name of system.                      |
| mediaPath  | string | Yes      | Path to media file on server.                |
| mediaName  | string | Yes      | Display name of media.                       |

### media.stopped

Media has stopped on server.

#### Response

Returns `null`.

### media.indexing

Sent during media database generation to indicate indexing progress and completion status.

#### Parameters

| Key                | Type    | Required | Description                                    |
| :----------------- | :------ | :------- | :--------------------------------------------- |
| exists             | boolean | Yes      | True if media database exists                  |
| indexing           | boolean | Yes      | True if indexing is currently in progress      |
| totalSteps         | number  | No       | Total number of systems to process             |
| currentStep        | number  | No       | Current system being processed (1-based)       |
| currentStepDisplay | string  | No       | Display name of current system being processed |
| totalFiles         | number  | No       | Total number of media files discovered         |

Progress can be tracked using `currentStep` out of `totalSteps` systems processed.
