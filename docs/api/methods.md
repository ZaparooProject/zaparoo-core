# Methods

Methods are used to execute actions and request data back from the API.

## Launching

### run

Emulate the scanning of a token.

#### Parameters

Accepts two types of parameters:

- A string, in which case the string will be treated as the token text with all other options set as default.
- An object:

| Key    | Type    | Required | Description                                                                                                    |
| :----- | :------ | :------- | :------------------------------------------------------------------------------------------------------------- |
| type   | string  | No       | An internal category of the type of token being scanned. _Not currently in use outside of logging._            |
| uid    | string  | No\*     | The UID of the token being scanned. For example, the UID of an NFC tag. Used for matching mappings.            |
| text   | string  | No\*     | The main text to be processed from a scan, should contain [ZapScript](../../zapscript/index.md).               |
| data   | string  | No\*     | The raw data read from a token, converted to a hexadecimal string. Used in mappings and detection of NFC toys. |
| unsafe | boolean | No       | Allow unsafe operations. Default is false.                                                                     |

These parameters allow emulating a token exactly as it would be read directly from an attached reader on the server. A request's parameters must contain at least a populated `uid`, `text` or `data` value.

#### Result

Returns `null` on success.

Currently, it is not reported if the launched ZapScript encountered an error during launching, and the method will return before execution of ZapScript is complete.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "52f6242e-7a5a-11ef-bf93-020304050607",
  "method": "run",
  "params": {
    "text": "**launch.system:snes"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "52f6242e-7a5a-11ef-bf93-020304050607",
  "result": null
}
```

### stop

Kill any active launcher, if possible.

This method is highly dependant on the platform and specific launcher used. It's not guaranteed that a launcher is capable of killing the playing process.

#### Parameters

None.

#### Result

Returns `null` on success.

Currently, it is not reported if a process was killed or not.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "176b4558-7a5b-11ef-b318-020304050607",
  "method": "stop"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "176b4558-7a5b-11ef-b318-020304050607",
  "result": null
}
```

## Tokens

### tokens

Returns information about active and last scanned tokens.

#### Parameters

None.

#### Result

| Key    | Type                            | Required | Description                                                    |
| :----- | :------------------------------ | :------- | :------------------------------------------------------------- |
| active | [TokenResponse](#token-object)[] | Yes      | A list of currently active tokens.                             |
| last   | [TokenResponse](#token-object)   | No       | The last scanned token. Null if no token has been scanned yet. |

##### Token object

| Key      | Type    | Required | Description                                      |
| :------- | :------ | :------- | :----------------------------------------------- |
| type     | string  | Yes      | Type of token.                                   |
| uid      | string  | Yes      | UID of the token.                                |
| text     | string  | Yes      | Text content of the token.                       |
| data     | string  | Yes      | Raw data of the token as hexadecimal string.     |
| scanTime | string  | Yes      | Timestamp of when the token was scanned in RFC3339 format. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "5e9f3a0e-7a5b-11ef-8084-020304050607",
  "method": "tokens"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "5e9f3a0e-7a5b-11ef-8084-020304050607",
  "result": {
    "active": [],
    "last": {
      "type": "",
      "uid": "",
      "text": "**launch.system:snes",
      "data": "",
      "scanTime": "2024-09-24T17:49:42.938167429+08:00"
    }
  }
}
```

### tokens.history

Returns a list of the last recorded token launches.

#### Parameters

None.

#### Result

| Key     | Type                                  | Required | Description                        |
| :------ | :------------------------------------ | :------- | :--------------------------------- |
| entries | [LaunchEntry](#launch-entry-object)[] | Yes      | A list of recorded token launches. |

##### Launch entry object

| Key     | Type    | Required | Description                                     |
| :------ | :------ | :------- | :---------------------------------------------- |
| data    | string  | Yes      | Raw data of the token as hexadecimal string.    |
| success | boolean | Yes      | True if the launch was successful.              |
| text    | string  | Yes      | Text content of the token.                      |
| time    | string  | Yes      | Timestamp of the launch time in RFC3339 format. |
| type    | string  | Yes      | Type of token.                                  |
| uid     | string  | Yes      | UID of the token.                               |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "5e9f3a0e-7a5b-11ef-8084-020304050607",
  "method": "tokens.history"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "5e9f3a0e-7a5b-11ef-8084-020304050607",
  "result": {
    "entries": [
      {
        "data": "",
        "success": true,
        "text": "**launch.system:snes",
        "time": "2024-09-24T17:49:42.938167429+08:00",
        "type": "",
        "uid": ""
      }
    ]
  }
}
```

## Media

### media

Returns the current media database status and active media.

#### Parameters

None.

#### Result

| Key      | Type                                      | Required | Description                            |
| :------- | :---------------------------------------- | :------- | :------------------------------------- |
| database | [IndexingStatus](#indexing-status-object) | Yes      | Status of the media database.           |
| active   | [ActiveMedia](#active-media-object)[]     | Yes      | List of currently active media.         |

##### Indexing status object

| Key                | Type   | Required | Description                                      |
| :----------------- | :----- | :------- | :----------------------------------------------- |
| exists             | boolean| Yes      | True if the database exists.                     |
| indexing           | boolean| Yes      | True if indexing is currently in progress.       |
| totalSteps         | number | No       | Total number of indexing steps.                 |
| currentStep        | number | No       | Current indexing step.                          |
| currentStepDisplay | string | No       | Display name of the current indexing step.      |
| totalFiles         | number | No       | Total number of files to index.                 |

##### Active media object

| Key        | Type   | Required | Description                                |
| :--------- | :----- | :------- | :----------------------------------------- |
| launcherId | string | Yes      | ID of the launcher.                        |
| systemId   | string | Yes      | ID of the system.                          |
| systemName | string | Yes      | Display name of the system.                |
| mediaPath  | string | Yes      | Path to the media file.                    |
| mediaName  | string | Yes      | Display name of the media.                 |
| started    | string | Yes      | Timestamp when media started in RFC3339 format. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "method": "media"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": {
    "database": {
      "exists": true,
      "indexing": false
    },
    "active": []
  }
}
```

### media.search

Query the media database and return all matching indexed media.

#### Parameters

An object:

| Key        | Type     | Required | Description                                                                                                                    |
| :--------- | :------- | :------- | :----------------------------------------------------------------------------------------------------------------------------- |
| query      | string   | Yes      | Case-insensitive search by filename. By default, query is split by white space and results are found which contain every word. |
| systems    | string[] | No       | Case-sensitive list of system IDs to restrict search to. A missing key or empty list will search all systems.                  |
| maxResults | number   | No       | Max number of results to return. Default is 250.                                                                               |

#### Result

| Key     | Type    | Required | Description                                        |
| :------ | :------ | :------- | :------------------------------------------------- |
| results | Media[] | Yes      | A list of all search results from the given query. |
| total   | number  | Yes      | Total number of search results.                    |

##### Media object

| Key    | Type                     | Required | Description                                                                                                 |
| :----- | :----------------------- | :------- | :---------------------------------------------------------------------------------------------------------- |
| system | [System](#system-object) | Yes      | System which the media has been indexed under.                                                              |
| name   | string                   | Yes      | A human-readable version of the result's filename without a file extension.                                 |
| path   | string                   | Yes      | Path to the media file. If possible, this path will be compressed into the `<system>/<path>` launch format. |

##### System object

| Key      | Type   | Required | Description                                           |
| :------- | :----- | :------- | :---------------------------------------------------- |
| id       | string | Yes      | Internal system ID for this system.                   |
| name     | string | Yes      | Display name of the system.                           |
| category | string | Yes      | Category of system. This field is not yet formalised. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "method": "media.search",
  "params": {
    "query": "240p"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": {
    "results": [
      {
        "name": "240p Test Suite (PD) v0.03 tepples",
        "path": "Gameboy/240p Test Suite (PD) v0.03 tepples.gb",
        "system": {
          "category": "Handheld",
          "id": "Gameboy",
          "name": "Gameboy"
        }
      }
    ],
    "total": 1
  }
}
```

### media.generate

Create a new media database index.

During indexing, the server will emit [media.indexing](./notifications.md) notifications showing progress of the index.

#### Parameters

Optionally, an object:

| Key     | Type     | Required | Description                                                                         |
| :------ | :------- | :------- | :---------------------------------------------------------------------------------- |
| systems | string[] | No       | List of system IDs to restrict indexing to. Other system indexes will remain as is. |

An omitted or `null` value parameters key is also valid and will index every system.

#### Result

Returns `null` on success once indexing is complete.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "6f20e07c-7a5e-11ef-84bb-020304050607",
  "method": "media.generate"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "6f20e07c-7a5e-11ef-84bb-020304050607",
  "result": null
}
```

### media.active

Returns the currently active media.

#### Parameters

None.

#### Result

Returns a list of [ActiveMedia](#active-media-object) objects or an empty array if no media is active.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "method": "media.active"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": []
}
```

### media.active.update

Update the currently active media information.

#### Parameters

An object:

| Key       | Type   | Required | Description                 |
| :-------- | :----- | :------- | :-------------------------- |
| systemId  | string | Yes      | ID of the system.           |
| mediaPath | string | Yes      | Path to the media file.     |
| mediaName | string | Yes      | Display name of the media.  |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "method": "media.active.update",
  "params": {
    "systemId": "SNES",
    "mediaPath": "/roms/snes/game.sfc",
    "mediaName": "Game"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": null
}
```

### systems

List all currently indexed systems.

#### Parameters

None.

#### Result

| Key     | Type                       | Required | Description                    |
| :------ | :------------------------- | :------- | :----------------------------- |
| systems | [System](#system-object)[] | Yes      | A list of all indexed systems. |

See [System object](#system-object).

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "dbd312f3-7a5f-11ef-8f29-020304050607",
  "method": "systems"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "dbd312f3-7a5f-11ef-8f29-020304050607",
  "result": {
    "systems": [
      {
        "category": "Handheld",
        "id": "GameboyColor",
        "name": "Gameboy Color"
      },
      {
        "category": "Computer",
        "id": "EDSAC",
        "name": "EDSAC"
      }
    ]
  }
}
```

## Settings

### settings

List currently set configuration settings.

This method will list values set in the [Config File](../../core/config.md). Some config file options may be omitted which are not appropriate to be read or written remotely.

#### Parameters

None.

#### Result

| Key                       | Type     | Required | Description                                                     |
| :------------------------ | :------- | :------- | :-------------------------------------------------------------- |
| runZapScript              | boolean  | Yes      | Whether ZapScript execution is enabled.                         |
| debugLogging              | boolean  | Yes      | Whether debug logging is enabled.                               |
| audioScanFeedback         | boolean  | Yes      | Whether audio feedback on scan is enabled.                      |
| readersAutoDetect         | boolean  | Yes      | Whether automatic reader detection is enabled.                  |
| readersScanMode           | string   | Yes      | Current scan mode setting.                                      |
| readersScanExitDelay      | number   | Yes      | Delay before exiting scan mode in seconds.                      |
| readersScanIgnoreSystems  | string[] | Yes      | List of system IDs to ignore during scanning.                   |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "f208d996-7ae6-11ef-960e-020304050607",
  "method": "settings"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "f208d996-7ae6-11ef-960e-020304050607",
  "result": {
    "runZapScript": true,
    "debugLogging": false,
    "audioScanFeedback": true,
    "readersAutoDetect": true,
    "readersScanMode": "insert",
    "readersScanExitDelay": 0.0,
    "readersScanIgnoreSystems": ["DOS"]
  }
}
```

### settings.update

Update one or more settings in-memory and save changes to disk.

This method will only write values which are supplied. Existing values will not be modified.

#### Parameters

An object containing any of the following optional keys:

| Key                       | Type     | Required | Description                                                     |
| :------------------------ | :------- | :------- | :-------------------------------------------------------------- |
| runZapScript              | boolean  | No       | Whether ZapScript execution is enabled.                         |
| debugLogging              | boolean  | No       | Whether debug logging is enabled.                               |
| audioScanFeedback         | boolean  | No       | Whether audio feedback on scan is enabled.                      |
| readersAutoDetect         | boolean  | No       | Whether automatic reader detection is enabled.                  |
| readersScanMode           | string   | No       | Current scan mode setting.                                      |
| readersScanExitDelay      | number   | No       | Delay before exiting scan mode in seconds.                      |
| readersScanIgnoreSystems  | string[] | No       | List of system IDs to ignore during scanning.                   |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "settings.update",
  "params": {
    "debugLogging": false
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": null
}
```

### settings.reload

Reload settings from the configuration file.

#### Parameters

None.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "settings.reload"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": null
}
```

## Mappings

Mappings are used to modify the contents of tokens before they're launched, based on different types of matching parameters. Stored mappings are queried before every launch and applied to the token if there's a match. This allows, for example, adding ZapScript to a read-only NFC tag based on its UID.

### mappings

List all mappings.

Returns a list of all active and inactive mappings entries stored on server.

#### Parameters

None.

#### Result

| Key      | Type                         | Required | Description                                                         |
| :------- | :--------------------------- | :------- | :------------------------------------------------------------------ |
| mappings | [Mapping](#mapping-object)[] | Yes      | List of all stored mappings. See [mapping object](#mapping-object). |

##### Mapping object

| Key      | Type    | Required | Description                                                                                                                                                                                                                                                                                                                                                             |
| :------- | :------ | :------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| id       | string  | Yes      | Internal database ID of mapping entry. Used to reference mapping for updates and deletions.                                                                                                                                                                                                                                                                             |
| added    | string  | Yes      | Timestamp of the time mapping was created in RFC3339 format.                                                                                                                                                                                                                                                                                                            |
| label    | string  | Yes      | An optional display name shown to the user.                                                                                                                                                                                                                                                                                                                             |
| enabled  | boolean | Yes      | True if the mapping will be used when looking up matching mappings.                                                                                                                                                                                                                                                                                                     |
| type     | string  | Yes      | The field which will be matched against:<br/>_ `uid`: match on UID, if available. UIDs are normalized before matching to remove spaces, colons and convert to lowercase.<br/>_ `text`: match on the stored text on token.<br/>\* `data`: match on the raw token data, if available. This is converted from bytes to a hexadecimal string and should be matched as this. |
| match    | string  | Yes      | The method used to match a mapping pattern:<br/>_ `exact`: match the entire string exactly to the field.<br/>_ `partial`: match part of the string to the field.<br/>\* `regex`: use a regular expression to match the field.                                                                                                                                           |
| pattern  | string  | Yes      | Pattern that will be matched against the token, using the above settings.                                                                                                                                                                                                                                                                                               |
| override | string  | Yes      | Final text that will completely replace the existing token text if a match was successful.                                                                                                                                                                                                                                                                              |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "1a8bee28-7aef-11ef-8427-020304050607",
  "method": "mappings"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "1a8bee28-7aef-11ef-8427-020304050607",
  "result": {
    "mappings": [
      {
        "id": "1",
        "added": "1970-01-21T06:08:18+08:00",
        "label": "barcode pokemon",
        "enabled": true,
        "type": "text",
        "match": "partial",
        "pattern": "9780307468031",
        "override": "**launch.search:gbc/*pokemon*gold*"
      }
    ]
  }
}
```

### mappings.new

Create a new mapping.

#### Parameters

An object:

| Key      | Type    | Required | Description                                                                                                                                                                                                                                                                                                                                                             |
| :------- | :------ | :------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| label    | string  | Yes      | An optional display name shown to the user.                                                                                                                                                                                                                                                                                                                             |
| enabled  | boolean | Yes      | True if the mapping will be used when looking up matching mappings.                                                                                                                                                                                                                                                                                                     |
| type     | string  | Yes      | The field which will be matched against:<br/>_ `uid`: match on UID, if available. UIDs are normalized before matching to remove spaces, colons and convert to lowercase.<br/>_ `text`: match on the stored text on token.<br/>\* `data`: match on the raw token data, if available. This is converted from bytes to a hexadecimal string and should be matched as this. |
| match    | string  | Yes      | The method used to match a mapping pattern:<br/>_ `exact`: match the entire string exactly to the field.<br/>_ `partial`: match part of the string to the field.<br/>\* `regex`: use a regular expression to match the field.                                                                                                                                           |
| pattern  | string  | Yes      | Pattern that will be matched against the token, using the above settings.                                                                                                                                                                                                                                                                                               |
| override | string  | Yes      | Final text that will completely replace the existing token text if a match was successful.                                                                                                                                                                                                                                                                              |

#### Result

| Key | Type   | Required | Description                       |
| :-- | :----- | :------- | :-------------------------------- |
| id  | string | Yes      | Database ID of new mapping entry. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "mappings.new",
  "params": {
    "label": "Test Mapping",
    "enabled": true,
    "type": "text",
    "match": "exact",
    "pattern": "test",
    "override": "**launch.system:snes"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": {
    "id": "2"
  }
}
```

### mappings.delete

Delete an existing mapping.

#### Parameters

An object:

| Key | Type   | Required | Description             |
| :-- | :----- | :------- | :---------------------- |
| id  | number | Yes      | Database ID of mapping. |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "mappings.delete",
  "params": {
    "id": 1
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": null
}
```

### mappings.update

Change an existing mapping.

#### Parameters

An object:

| Key      | Type    | Required | Description                                                                                                                                                                                                                                                                                                                                                             |
| :------- | :------ | :------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| id       | number  | Yes      | Internal database ID of mapping entry.                                                                                                                                                                                                                                                                                                                                  |
| label    | string  | No       | An optional display name shown to the user.                                                                                                                                                                                                                                                                                                                             |
| enabled  | boolean | No       | True if the mapping will be used when looking up matching mappings.                                                                                                                                                                                                                                                                                                     |
| type     | string  | No       | The field which will be matched against:<br/>_ `uid`: match on UID, if available. UIDs are normalized before matching to remove spaces, colons and convert to lowercase.<br/>_ `text`: match on the stored text on token.<br/>\* `data`: match on the raw token data, if available. This is converted from bytes to a hexadecimal string and should be matched as this. |
| match    | string  | No       | The method used to match a mapping pattern:<br/>_ `exact`: match the entire string exactly to the field.<br/>_ `partial`: match part of the string to the field.<br/>\* `regex`: use a regular expression to match the field.                                                                                                                                           |
| pattern  | string  | No       | Pattern that will be matched against the token, using the above settings.                                                                                                                                                                                                                                                                                               |
| override | string  | No       | Final text that will completely replace the existing token text if a match was successful.                                                                                                                                                                                                                                                                              |

Only keys which are provided in the object will be updated in the database.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "e98fd686-7e62-11ef-8f8c-020304050607",
  "method": "mappings.update",
  "params": {
    "id": 1,
    "enabled": false
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "e98fd686-7e62-11ef-8f8c-020304050607",
  "result": null
}
```

### mappings.reload

Reload mappings from the configuration file.

#### Parameters

None.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "mappings.reload"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": null
}
```

## Readers

### readers.write

Attempt to write given text to the first available write-capable reader, if possible.

#### Parameters

An object:

| Key  | Type   | Required | Description                           |
| :--- | :----- | :------- | :------------------------------------ |
| text | string | Yes      | ZapScript to be written to the token. |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "readers.write",
  "params": {
    "text": "**launch.system:snes"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": null
}
```

### readers.write.cancel

Cancel any ongoing write operation.

#### Parameters

None.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "readers.write.cancel"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": null
}
```

## Service

### version

Return server's current version and platform.

#### Parameters

None.

#### Result

| Key      | Type   | Required | Description                                         |
| :------- | :----- | :------- | :-------------------------------------------------- |
| platform | string | Yes      | ID of platform the service is currently running on. |
| version  | string | Yes      | Current version of the running Zaparoo service.     |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "ca47f646-7e47-11ef-971a-020304050607",
  "method": "version"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "ca47f646-7e47-11ef-971a-020304050607",
  "result": {
    "platform": "mister",
    "version": "2.0.0-dev"
  }
}
```