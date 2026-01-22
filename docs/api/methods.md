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

The database status includes both indexing and optimization information:
- **Indexing** takes priority over optimization in the response (if both are running, only indexing status is shown)
- **Optimization** status and progress are shown when no indexing is in progress

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
| optimizing         | boolean| Yes      | True if database optimization is currently in progress. |
| totalSteps         | number | No       | Total number of indexing steps.                 |
| currentStep        | number | No       | Current indexing step.                          |
| currentStepDisplay | string | No       | Display name of the current indexing step or optimization step. |
| totalFiles         | number | No       | Total number of files to index.                 |
| totalMedia         | number | No       | Total number of media entries in the database. Only included when database exists and is not indexing. |

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

##### Response (Database Ready)

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": {
    "database": {
      "exists": true,
      "indexing": false,
      "optimizing": false,
      "totalMedia": 1337
    },
    "active": []
  }
}
```

##### Response (Optimization in Progress)

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": {
    "database": {
      "exists": true,
      "indexing": false,
      "optimizing": true,
      "currentStepDisplay": "vacuum",
      "totalMedia": 1337
    },
    "active": []
  }
}
```

### media.search

Query the media database and return all matching indexed media.

**Note:** This API now uses cursor-based pagination for all requests. The `total` field is deprecated and always returns -1. Use the `pagination` object to navigate through results. For subsequent pages, include the `nextCursor` value in the `cursor` parameter of your next request.

#### Parameters

An object:

| Key        | Type     | Required | Description                                                                                                                    |
| :--------- | :------- | :------- | :----------------------------------------------------------------------------------------------------------------------------- |
| query      | string   | No       | Case-insensitive search by filename. By default, query is split by white space and results are found which contain every word. If omitted, all media is returned. |
| systems    | string[] | No       | Case-sensitive list of system IDs to restrict search to. A missing key or empty list will search all systems.                  |
| maxResults | number   | No       | Max number of results to return. Default is 100.                                                                               |
| cursor     | string   | No       | Cursor for pagination. Omit for first page, use `nextCursor` from previous response for subsequent pages.                     |
| tags       | string[] | No       | Filter results by tags. Maximum 50 tags, each up to 128 characters. Tags are case-sensitive and results must match all provided tags. Can be used without query or systems for tag-only searches. |
| letter     | string   | No       | Filter results by first character of game name. Supports: A-Z (single letters), "0-9" (numbers), "#" (symbols). Case-insensitive. |

#### Result

| Key        | Type                               | Required | Description                                                                     |
| :--------- | :--------------------------------- | :------- | :------------------------------------------------------------------------------ |
| results    | Media[]                            | Yes      | A list of all search results from the given query.                              |
| total      | number                             | Yes      | **Deprecated:** Returns the count of results in the current response page. Use pagination info for navigation. |
| pagination | [Pagination](#pagination-object)   | Yes      | Pagination information for cursor-based navigation.                             |

##### Media object

| Key    | Type                     | Required | Description                                                                                                 |
| :----- | :----------------------- | :------- | :---------------------------------------------------------------------------------------------------------- |
| system | [System](#system-object) | Yes      | System which the media has been indexed under.                                                              |
| name   | string                   | Yes      | A human-readable version of the result's filename without a file extension.                                 |
| path   | string                   | Yes      | Path to the media file. If possible, this path will be compressed into the `<system>/<path>` launch format. |
| tags   | [TagInfo](#taginfo-object)[] | Yes      | Array of tags associated with this media item.                                               |

##### System object

| Key          | Type   | Required | Description                                                              |
| :----------- | :----- | :------- | :----------------------------------------------------------------------- |
| id           | string | No       | Internal system ID for this system.                                      |
| name         | string | No       | Display name of the system.                                              |
| category     | string | No       | Category of system (e.g., "Console", "Computer"). Not yet formalised.    |
| releaseDate  | string | No       | Release date of the system in ISO 8601 format (YYYY-MM-DD).              |
| manufacturer | string | No       | Manufacturer of the system (e.g., "Nintendo", "Sega").                   |

##### Pagination object

| Key         | Type    | Required | Description                                                                 |
| :---------- | :------ | :------- | :-------------------------------------------------------------------------- |
| nextCursor  | string  | No       | Cursor for the next page of results. `null` if no more pages available.    |
| hasNextPage | boolean | Yes      | Whether there are more results available after the current page.           |
| pageSize    | number  | Yes      | Number of results requested for this page (matches `maxResults` parameter). |

##### TagInfo object

| Key  | Type   | Required | Description                                           |
| :--- | :----- | :------- | :---------------------------------------------------- |
| tag  | string | Yes      | The tag name.                                         |
| type | string | Yes      | The type/category of the tag (e.g., "genre", "year"). |

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
        },
        "tags": [
          {
            "tag": "test",
            "type": "category"
          },
          {
            "tag": "homebrew",
            "type": "category"
          }
        ]
      }
    ],
    "total": 1,
    "pagination": {
      "nextCursor": null,
      "hasNextPage": false,
      "pageSize": 100
    }
  }
}
```

##### Example with tag filtering

###### Request

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5d-11ef-9c7b-020304050607",
  "method": "media.search",
  "params": {
    "query": "mario",
    "tags": ["platformer", "nintendo"],
    "maxResults": 10
  }
}
```

###### Response

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5d-11ef-9c7b-020304050607",
  "result": {
    "results": [
      {
        "name": "Super Mario Bros.",
        "path": "NES/Super Mario Bros.nes",
        "system": {
          "category": "Console",
          "id": "NES",
          "name": "Nintendo Entertainment System"
        },
        "tags": [
          {
            "tag": "platformer",
            "type": "genre"
          },
          {
            "tag": "nintendo",
            "type": "publisher"
          },
          {
            "tag": "1985",
            "type": "year"
          }
        ]
      }
    ],
    "total": 1,
    "pagination": {
      "nextCursor": null,
      "hasNextPage": false,
      "pageSize": 10
    }
  }
}
```

### media.tags

Query the media database and return available tags for filtering.

This method returns all available tags (with their types) for the specified systems. Use this to build dynamic filter UIs showing available tag options.

#### Parameters

| Key     | Type     | Required | Description                                                                                         |
| :------ | :------- | :------- | :-------------------------------------------------------------------------------------------------- |
| systems | string[] | No       | Case-sensitive list of system IDs to restrict tags to. A missing key or empty list will get all systems. |

#### Result

| Key  | Type                     | Required | Description                    |
| :--- | :----------------------- | :------- | :----------------------------- |
| tags | [TagInfo](#taginfo-object)[] | Yes      | Array of available tags.       |

##### TagInfo object

| Key  | Type   | Required | Description                           |
| :--- | :----- | :------- | :------------------------------------ |
| tag  | string | Yes      | The tag value.                        |
| type | string | Yes      | The tag type (e.g., "genre", "year"). |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5d-11ef-9c7b-020304050607",
  "method": "media.tags",
  "params": {
    "systems": ["NES", "SNES"]
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5d-11ef-9c7b-020304050607",
  "result": {
    "tags": [
      {
        "type": "genre",
        "tag": "action"
      },
      {
        "type": "genre",
        "tag": "platformer"
      },
      {
        "type": "series",
        "tag": "Mario Bros"
      },
      {
        "type": "series",
        "tag": "Super Mario"
      }
    ]
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

**Selective Indexing Behavior:**
- When `systems` is provided with specific system IDs, only those systems will be reindexed
- The server will validate all provided system IDs and return an error if any are invalid
- If all systems are specified (equivalent to no restriction), a full database rebuild will be performed for optimal performance
- Selective indexing cannot be performed while database optimization is running
- Resume functionality will validate that the system configuration hasn't changed between indexing sessions

#### Result

Returns `null` on success once indexing is complete.

#### Examples

##### Full Index Request

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

##### Selective Index Request

```json
{
  "jsonrpc": "2.0",
  "id": "7f30e17d-7a5e-11ef-85cc-020304050607",
  "method": "media.generate",
  "params": {
    "systems": ["NES", "SNES", "Genesis"]
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "7f30e17d-7a5e-11ef-85cc-020304050607",
  "result": null
}
```

### media.generate.cancel

Cancel any currently running media database indexing operation.

#### Parameters

None.

#### Result

| Key     | Type   | Required | Description                           |
| :------ | :----- | :------- | :------------------------------------ |
| message | string | Yes      | Status message about the cancellation. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "8f40e28e-7a5e-11ef-86dd-020304050607",
  "method": "media.generate.cancel"
}
```

##### Response (Indexing was running)

```json
{
  "jsonrpc": "2.0",
  "id": "8f40e28e-7a5e-11ef-86dd-020304050607",
  "result": {
    "message": "Media indexing cancelled successfully"
  }
}
```

##### Response (No indexing running)

```json
{
  "jsonrpc": "2.0",
  "id": "8f40e28e-7a5e-11ef-86dd-020304050607",
  "result": {
    "message": "No media indexing operation is currently running"
  }
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
        "id": "GameboyColor",
        "name": "Gameboy Color",
        "category": "Handheld",
        "releaseDate": "1998-10-21",
        "manufacturer": "Nintendo"
      },
      {
        "id": "EDSAC",
        "name": "EDSAC",
        "category": "Computer",
        "releaseDate": "1949-05-06",
        "manufacturer": "University of Cambridge"
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

| Key                       | Type                                      | Required | Description                                                     |
| :------------------------ | :---------------------------------------- | :------- | :-------------------------------------------------------------- |
| runZapScript              | boolean                                   | Yes      | Whether ZapScript execution is enabled.                         |
| debugLogging              | boolean                                   | Yes      | Whether debug logging is enabled.                               |
| audioScanFeedback         | boolean                                   | Yes      | Whether audio feedback on scan is enabled.                      |
| readersAutoDetect         | boolean                                   | Yes      | Whether automatic reader detection is enabled.                  |
| readersScanMode           | string                                    | Yes      | Current scan mode setting.                                      |
| readersScanExitDelay      | number                                    | Yes      | Delay before exiting scan mode in seconds.                      |
| readersScanIgnoreSystems  | string[]                                  | Yes      | List of system IDs to ignore during scanning.                   |
| errorReporting            | boolean                                   | Yes      | Whether error reporting is enabled.                             |
| readersConnect            | [ReaderConnection](#reader-connection-object)[] | Yes      | List of manually configured reader connections.                 |

##### Reader connection object

| Key      | Type   | Required | Description                                      |
| :------- | :----- | :------- | :----------------------------------------------- |
| driver   | string | Yes      | Reader driver type (e.g., "pn532uart", "acr122pcsc"). |
| path     | string | Yes      | Path or address for the reader connection.       |
| idSource | string | No       | Source for the reader ID.                        |

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
    "readersScanIgnoreSystems": ["DOS"],
    "errorReporting": true,
    "readersConnect": []
  }
}
```

### settings.update

Update one or more settings in-memory and save changes to disk.

This method will only write values which are supplied. Existing values will not be modified.

#### Parameters

An object containing any of the following optional keys:

| Key                       | Type                                      | Required | Description                                                     |
| :------------------------ | :---------------------------------------- | :------- | :-------------------------------------------------------------- |
| runZapScript              | boolean                                   | No       | Whether ZapScript execution is enabled.                         |
| debugLogging              | boolean                                   | No       | Whether debug logging is enabled.                               |
| audioScanFeedback         | boolean                                   | No       | Whether audio feedback on scan is enabled.                      |
| readersAutoDetect         | boolean                                   | No       | Whether automatic reader detection is enabled.                  |
| readersScanMode           | string                                    | No       | Current scan mode setting.                                      |
| readersScanExitDelay      | number                                    | No       | Delay before exiting scan mode in seconds.                      |
| readersScanIgnoreSystems  | string[]                                  | No       | List of system IDs to ignore during scanning.                   |
| errorReporting            | boolean                                   | No       | Whether error reporting is enabled.                             |
| readersConnect            | [ReaderConnection](#reader-connection-object)[] | No       | List of manually configured reader connections.                 |

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

### settings.logs.download

Download the current log file as base64-encoded content.

#### Parameters

None.

#### Result

| Key      | Type   | Required | Description                                      |
| :------- | :----- | :------- | :----------------------------------------------- |
| filename | string | Yes      | Name of the log file.                            |
| size     | number | Yes      | Size of the log file in bytes.                   |
| content  | string | Yes      | Base64-encoded content of the log file.          |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "9f50e39f-7a5e-11ef-87ee-020304050607",
  "method": "settings.logs.download"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "9f50e39f-7a5e-11ef-87ee-020304050607",
  "result": {
    "filename": "zaparoo.log",
    "size": 1024,
    "content": "MjAyNC0wOS0yNFQxNzowMDowMC4wMDBaIElORk8gU3RhcnRpbmcgWmFwYXJvby4uLg=="
  }
}
```

## Playtime

### playtime

Query current playtime session status and usage statistics.

This method returns comprehensive information about the current playtime session, including active game time, cumulative session time, cooldown state, daily usage, and remaining time before limits are reached.

**Session States:**
- `reset` - No active session, ready to start new session
- `active` - Game currently running, time being tracked
- `cooldown` - Game stopped but session persists (within session reset timeout)

#### Parameters

None.

#### Result

| Key                   | Type    | Required | Description                                                                                           |
| :-------------------- | :------ | :------- | :---------------------------------------------------------------------------------------------------- |
| state                 | string  | Yes      | Current session state: `"reset"`, `"active"`, or `"cooldown"`.                                        |
| sessionActive         | boolean | Yes      | Whether a game is currently running.                                                                  |
| limitsEnabled         | boolean | Yes      | Whether playtime limits are currently enabled for enforcement.                                        |
| sessionStarted        | string  | No       | ISO 8601 timestamp when current game started. Only present during `"active"` state.                   |
| sessionDuration       | string  | No       | Total time in current session (Go duration format). Present during `"active"` and `"cooldown"` states. |
| sessionCumulativeTime | string  | No       | Cumulative time from previous games in session. Present during `"active"` and `"cooldown"` states.    |
| sessionRemaining      | string  | No       | Time remaining before session limit reached. Only present if session limit is configured.             |
| cooldownRemaining     | string  | No       | Time until session auto-resets. Only present during `"cooldown"` state.                               |
| dailyUsageToday       | string  | No       | Total playtime accumulated today. Available in all states when data is available.                     |
| dailyRemaining        | string  | No       | Time remaining before daily limit reached. Available in all states if daily limit is configured.      |

**Note:** All duration fields use Go's duration format (e.g., `"1h30m45s"`, `"45m"`, `"2h"`).

#### Examples

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5e-11ef-9c7b-020304050607",
  "method": "playtime"
}
```

##### Response (Reset State)

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5e-11ef-9c7b-020304050607",
  "result": {
    "state": "reset",
    "sessionActive": false,
    "limitsEnabled": true,
    "dailyUsageToday": "1h30m0s",
    "dailyRemaining": "2h30m0s"
  }
}
```

##### Response (Active Game with Limits)

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5e-11ef-9c7b-020304050607",
  "result": {
    "state": "active",
    "sessionActive": true,
    "limitsEnabled": true,
    "sessionStarted": "2025-01-22T14:30:00Z",
    "sessionDuration": "45m30s",
    "sessionCumulativeTime": "15m",
    "sessionRemaining": "14m30s",
    "dailyUsageToday": "2h15m30s",
    "dailyRemaining": "1h44m30s"
  }
}
```

##### Response (Cooldown State)

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5e-11ef-9c7b-020304050607",
  "result": {
    "state": "cooldown",
    "sessionActive": false,
    "limitsEnabled": true,
    "sessionDuration": "45m30s",
    "sessionCumulativeTime": "45m30s",
    "sessionRemaining": "14m30s",
    "cooldownRemaining": "12m30s",
    "dailyUsageToday": "2h15m30s",
    "dailyRemaining": "1h44m30s"
  }
}
```

### settings.playtime.limits

Get current playtime limit configuration.

Returns all configured playtime limits including daily limits, session limits, session reset timeout, warning intervals, and retention settings.

#### Parameters

None.

#### Result

| Key          | Type     | Required | Description                                                                                                            |
| :----------- | :------- | :------- | :--------------------------------------------------------------------------------------------------------------------- |
| enabled      | boolean  | Yes      | Whether playtime limits are enabled for enforcement.                                                                   |
| daily        | string   | No       | Daily playtime limit in Go duration format (e.g., `"4h"`). Omitted if not configured.                                 |
| session      | string   | No       | Per-session playtime limit in Go duration format (e.g., `"1h"`). Omitted if not configured.                           |
| sessionReset | string   | No       | Idle timeout before session auto-resets in Go duration format (e.g., `"20m"`). `"0s"` means session never resets.     |
| warnings     | string[] | Yes      | List of time intervals when warnings are sent before limits reached (e.g., `["5m", "2m", "1m"]`). Empty array if none. |
| retention    | number   | No       | Number of days to retain playtime history. Omitted if not configured.                                                  |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5e-11ef-9c7b-020304050607",
  "method": "settings.playtime.limits"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5e-11ef-9c7b-020304050607",
  "result": {
    "enabled": true,
    "daily": "4h",
    "session": "1h",
    "sessionReset": "20m",
    "warnings": ["5m", "2m", "1m"],
    "retention": 30
  }
}
```

### settings.playtime.limits.update

Update playtime limit settings.

This method updates one or more playtime limit configuration values in-memory and saves changes to disk. Only provided fields will be updated; omitted fields remain unchanged.

#### Parameters

An object containing any of the following optional keys:

| Key          | Type     | Required | Description                                                                                                                        |
| :----------- | :------- | :------- | :--------------------------------------------------------------------------------------------------------------------------------- |
| enabled      | boolean  | No       | Enable or disable playtime limit enforcement.                                                                                      |
| daily        | string   | No       | Daily playtime limit in Go duration format (e.g., `"4h"`, `"2h30m"`). Use `"0"` or `"0s"` to disable daily limit.                 |
| session      | string   | No       | Per-session playtime limit in Go duration format (e.g., `"1h"`, `"45m"`). Use `"0"` or `"0s"` to disable session limit.           |
| sessionReset | string   | No       | Idle timeout before session auto-resets in Go duration format (e.g., `"20m"`). Use `"0"` or `"0s"` for sessions that never reset. |
| warnings     | string[] | No       | List of time intervals for warnings in Go duration format (e.g., `["10m", "5m", "1m"]`). Empty array disables warnings.           |
| retention    | number   | No       | Number of days to retain playtime history. Use `0` for no retention limit.                                                        |

**Important:** Duration strings must use Go duration format: combinations of hours (`h`), minutes (`m`), and seconds (`s`). Examples: `"1h"`, `"30m"`, `"1h30m"`, `"2h15m30s"`.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "c3d4e5f6-7a5e-11ef-9c7b-020304050607",
  "method": "settings.playtime.limits.update",
  "params": {
    "enabled": true,
    "session": "1h",
    "warnings": ["10m", "5m", "2m"]
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "c3d4e5f6-7a5e-11ef-9c7b-020304050607",
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

### readers

List all currently connected readers and their capabilities.

#### Parameters

None.

#### Result

| Key     | Type                       | Required | Description                         |
| :------ | :------------------------- | :------- | :---------------------------------- |
| readers | [ReaderInfo](#reader-info-object)[] | Yes      | A list of all connected readers.    |

##### Reader info object

| Key          | Type     | Required | Description                                   |
| :----------- | :------- | :------- | :-------------------------------------------- |
| id           | string   | Yes      | Unique identifier for the reader.             |
| info         | string   | Yes      | Human-readable information about the reader.  |
| connected    | boolean  | Yes      | Whether the reader is currently connected.    |
| capabilities | string[] | Yes      | List of capabilities supported by the reader. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "method": "readers"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "562c0b60-7ae8-11ef-87d7-020304050607",
  "result": {
    "readers": [
      {
        "id": "pn532_1",
        "info": "PN532 NFC Reader",
        "connected": true,
        "capabilities": ["read", "write"]
      }
    ]
  }
}
```

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

## Launchers

### launchers.refresh

Refresh the internal launcher cache, forcing a reload of launcher configurations.

#### Parameters

None.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "af60e4a0-7a5e-11ef-88ff-020304050607",
  "method": "launchers.refresh"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "af60e4a0-7a5e-11ef-88ff-020304050607",
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

### health

Simple health check to verify the server is running and responding.

#### Parameters

None.

#### Result

| Key    | Type   | Required | Description                                      |
| :----- | :----- | :------- | :----------------------------------------------- |
| status | string | Yes      | Health status. Returns `"ok"` when server is healthy. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "db58f757-7e47-11ef-982b-020304050607",
  "method": "health"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "db58f757-7e47-11ef-982b-020304050607",
  "result": {
    "status": "ok"
  }
}
```

## Inbox

Inbox messages are system notifications stored on the server, typically used to inform the user of events like update availability, errors, or other important information.

### inbox

List all inbox messages.

#### Parameters

None.

#### Result

| Key      | Type                               | Required | Description               |
| :------- | :--------------------------------- | :------- | :------------------------ |
| messages | [InboxMessage](#inbox-message-object)[] | Yes      | List of inbox messages.   |

##### Inbox message object

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

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "ec69f868-7e47-11ef-993c-020304050607",
  "method": "inbox"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "ec69f868-7e47-11ef-993c-020304050607",
  "result": {
    "messages": [
      {
        "id": 1,
        "title": "Update Available",
        "body": "A new version of Zaparoo is available.",
        "severity": 0,
        "category": "update",
        "createdAt": "2024-09-24T17:49:42.938167429+08:00"
      }
    ]
  }
}
```

### inbox.delete

Delete a specific inbox message by ID.

#### Parameters

An object:

| Key | Type   | Required | Description                   |
| :-- | :----- | :------- | :---------------------------- |
| id  | number | Yes      | ID of the message to delete.  |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "fd7a0979-7e47-11ef-9a4d-020304050607",
  "method": "inbox.delete",
  "params": {
    "id": 1
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "fd7a0979-7e47-11ef-9a4d-020304050607",
  "result": null
}
```

### inbox.clear

Delete all inbox messages.

#### Parameters

None.

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "0e8b1a8a-7e48-11ef-9b5e-020304050607",
  "method": "inbox.clear"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "0e8b1a8a-7e48-11ef-9b5e-020304050607",
  "result": null
}