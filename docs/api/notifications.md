# Notifications

Notifications are sent from the server to connected clients to inform them of events.

## Launching

### running

Media started running on the server.

## Readers

### readers.added

A new reader was connected to the server.

### readers.removed

A connected reader was disconnected from the server.

## Tokens

### tokens.added

A token was detected by a connected reader.

### tokens.removed

A token was removed from a connected reader.

## Media

### media.started

New media was started on server.

#### Response

| Key        | Type   | Required | Description                                  |
| :--------- | :----- | :------- | :------------------------------------------- |
| systemId   | string | Yes      | Internal ID of system associated with media. |
| systemName | string | Yes      | Display name of system.                      |
| mediaName  | string | Yes      | Display name of media.                       |
| mediaPath  | string | Yes      | Path to media file on server.                |

### media.stopped

Media has stopped on server.

#### Response

Returns `null`.

### media.indexing

The state of the indexing process has changed.

**This payload will have significant changes before release.**

#### Response

| Key         | Type    | Required | Description                                                                   |
| :---------- | :------ | :------- | :---------------------------------------------------------------------------- |
| indexing    | boolean | Yes      | True if an index is in progress.                                              |
| totalSteps  | number  | Yes      | Total number of "steps" (systems plus database preparation) in index process. |
| currentStep | number  | Yes      | Current step in index.                                                        |
| currentDesc | string  | Yes      | Label of current step (in English).                                           |
| totalFiles  | number  | Yes      | Total files indexed so far.                                                   |