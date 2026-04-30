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

### confirm

Confirm and launch a staged token from the launch guard.

When launch guard is enabled and media is playing, scanned tokens are staged instead of launched immediately. This method confirms the currently staged token and launches it.

#### Parameters

None.

#### Result

Returns `null` on success. Returns an error if no token is currently staged.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5b-11ef-b318-020304050607",
  "method": "confirm"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5b-11ef-b318-020304050607",
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
| readerId | string  | No       | ID of the reader that scanned the token.         |

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

| Key              | Type     | Required | Description                                |
| :--------------- | :------- | :------- | :----------------------------------------- |
| launcherId       | string   | Yes      | ID of the launcher.                        |
| systemId         | string   | Yes      | ID of the system.                          |
| systemName       | string   | Yes      | Display name of the system.                |
| mediaPath        | string   | Yes      | Path to the media file.                    |
| mediaName        | string   | Yes      | Display name of the media.                 |
| started          | string   | Yes      | Timestamp when media started in RFC3339 format. |
| zapScript        | string   | Yes      | ZapScript command to launch this media item. |
| launcherControls | string[] | No       | List of control action names supported by the active launcher. Only present if the launcher supports controls. See [media.control](#mediacontrol). |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "method": "media"
}
```

##### Response (database ready)

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

##### Response (optimization in progress)

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
| fuzzySystem | boolean | No       | Enable fuzzy matching for system IDs in the `systems` array (e.g., `"snes"` matches `"SNES"`). |

#### Result

| Key        | Type                               | Required | Description                                                                     |
| :--------- | :--------------------------------- | :------- | :------------------------------------------------------------------------------ |
| results    | Media[]                            | Yes      | A list of all search results from the given query.                              |
| total      | number                             | Yes      | **Deprecated:** Returns the count of results in the current response page. Use pagination info for navigation. |
| pagination | [Pagination](#pagination-object)   | Yes      | Pagination information for cursor-based navigation.                             |

##### Media object

| Key       | Type                     | Required | Description                                                                                                 |
| :-------- | :----------------------- | :------- | :---------------------------------------------------------------------------------------------------------- |
| system    | [System](#system-object) | Yes      | System which the media has been indexed under.                                                              |
| name      | string                   | Yes      | A human-readable version of the result's filename without a file extension.                                 |
| path      | string                   | Yes      | Path to the media file. If possible, this path will be compressed into the `<system>/<path>` launch format. |
| zapScript | string                   | Yes      | ZapScript command to launch this media item.                                                                |
| tags      | [TagInfo](#taginfo-object)[] | Yes      | Array of tags associated with this media item.                                               |

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
| nextCursor  | string  | No       | Cursor for the next page of results. Omitted if no more pages available.   |
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
        "zapScript": "@Gameboy/240p Test Suite (PD) v0.03 tepples",
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
        "zapScript": "@NES/Super Mario Bros. (year:1985)",
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
      "hasNextPage": false,
      "pageSize": 10
    }
  }
}
```

### media.browse

Browse indexed media content by directory, similar to navigating a file manager. Supports filesystem paths, virtual URI schemes (e.g. `mame-arcade://`), and paginated results.

When called without a `path` parameter (or with an empty path), returns top-level root entries including filesystem roots and virtual scheme roots. When `systems` is provided without `path`, returns populated launcher routes for those systems only. Pass the same `systems` filter when browsing a returned route to keep shared paths scoped to the selected systems.

#### Parameters

All parameters are optional. When called with no parameters, returns root entries.

| Key        | Type   | Required | Description                                                                                                |
| :--------- | :----- | :------- | :--------------------------------------------------------------------------------------------------------- |
| path       | string | No       | Directory path to browse. Omit or set empty to list root entries. Supports filesystem paths and virtual URI schemes (e.g. `mame-arcade://`). |
| systems    | string[] | No     | Case-sensitive list of system IDs to restrict route discovery and browse results to. A missing key or empty list preserves unfiltered behavior. |
| fuzzySystem | boolean | No     | Enable fuzzy matching for system IDs in the `systems` array (e.g., `"snes"` matches `"SNES"`). |
| maxResults | number | No       | Maximum results per page. Default is 100, maximum is 1000.                                                 |
| cursor     | string | No       | Opaque pagination cursor from a previous response's `nextCursor`. Omit for first page. Cursors are valid only with the same path, systems, letter, and sort parameters. |
| letter     | string | No       | Filter results to entries starting with this letter.                                                       |
| sort       | string | No       | Sort order. One of: `name-asc` (default), `name-desc`, `filename-asc`, `filename-desc`. The `filename` variants sort by full file path. |

#### Result

| Key        | Type                                  | Required | Description                                                              |
| :--------- | :------------------------------------ | :------- | :----------------------------------------------------------------------- |
| path       | string                                | Yes      | The browsed directory path. Empty string when listing roots.             |
| entries    | [BrowseEntry](#browse-entry-object)[] | Yes      | Array of entries in the current path.                                    |
| totalFiles | number                                | Yes      | Total count of media files in the current directory (respects `letter` filter). |
| pagination | [Pagination](#browse-pagination-object) | No     | Pagination info. Omitted when there are no file results.                 |

##### Browse entry object

| Key          | Type     | Required | Description                                                                                      |
| :----------- | :------- | :------- | :----------------------------------------------------------------------------------------------- |
| name         | string   | Yes      | Display name of the entry.                                                                       |
| path         | string   | Yes      | Full path to the entry.                                                                          |
| type         | string   | Yes      | Entry type: `root`, `directory`, or `media`.                                                     |
| fileCount    | number   | No       | Number of files in this directory. Present on `root` and `directory` entries.                     |
| group        | string   | No       | Launcher group name. Present on virtual scheme `root` entries.                                   |
| systemId     | string   | No       | System ID for the media or single-system filtered route (e.g. `SNES`). Present on `media` entries and filtered `root` entries when exactly one system applies. |
| systemIds    | string[] | No       | System IDs represented by a filtered `root` or `directory` entry.                                |
| zapScript    | string   | No       | ZapScript command to launch this media. Present on `media` entries.                              |
| relativePath | string   | No       | Relative path from root directory. Present on `media` entries.                                   |
| tags         | object[] | No       | Tags attached to the media. Each object has `tag` (string) and `type` (string). Present on `media` entries. |

##### Browse pagination object

| Key         | Type   | Required | Description                                              |
| :---------- | :----- | :------- | :------------------------------------------------------- |
| hasNextPage | bool   | Yes      | Whether more results exist beyond the current page.      |
| pageSize    | number | Yes      | The requested page size.                                 |
| nextCursor  | string | No       | Opaque cursor for the next page. Absent on the last page. |

#### System route example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "media.browse",
  "params": {
    "systems": ["SNES"]
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "path": "",
    "entries": [
      {
        "name": "SNES",
        "path": "/roms/SNES",
        "type": "root",
        "fileCount": 150,
        "systemId": "SNES",
        "systemIds": ["SNES"]
      }
    ],
    "totalFiles": 0
  }
}
```

#### Browse path example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "media.browse",
  "params": {
    "path": "/roms/SNES",
    "maxResults": 3
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "path": "/roms/SNES",
    "entries": [
      {
        "name": "RPGs",
        "path": "/roms/SNES/RPGs",
        "type": "directory",
        "fileCount": 42
      },
      {
        "name": "Super Mario World",
        "path": "/roms/SNES/Super Mario World.sfc",
        "type": "media",
        "systemId": "SNES",
        "zapScript": "@SNES/Super Mario World",
        "relativePath": "Super Mario World.sfc",
        "tags": [
          {"tag": "1990", "type": "year"},
          {"tag": "2", "type": "players"}
        ]
      },
      {
        "name": "The Legend of Zelda - A Link to the Past",
        "path": "/roms/SNES/The Legend of Zelda - A Link to the Past.sfc",
        "type": "media",
        "systemId": "SNES",
        "zapScript": "@SNES/The Legend of Zelda - A Link to the Past",
        "relativePath": "The Legend of Zelda - A Link to the Past.sfc",
        "tags": [
          {"tag": "1991", "type": "year"},
          {"tag": "1", "type": "players"}
        ]
      }
    ],
    "totalFiles": 150,
    "pagination": {
      "hasNextPage": true,
      "pageSize": 3,
      "nextCursor": "eyJzb3J0VmFsdWUiOiJUaGUgTGVnZW5kIG9mIFplbGRhIC0gQSBMaW5rIHRvIHRoZSBQYXN0IiwibGFzdElkIjo0Mn0="
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
| systems     | string[] | No       | Case-sensitive list of system IDs to restrict tags to. A missing key or empty list will get all systems. |
| fuzzySystem | boolean  | No       | Enable fuzzy matching for system IDs in the `systems` array (e.g., `"snes"` matches `"SNES"`). |

#### Result

| Key  | Type                     | Required | Description                    |
| :--- | :----------------------- | :------- | :----------------------------- |
| tags | [TagInfo](#taginfo-object)[] | Yes      | Array of available tags.       |

**Tag Capping:** To prevent large responses, long-tail tag types are capped at 100 entries
per type. Tags within each type are sorted by usage count (most popular first), then
alphabetically. The following types are capped: `credit`, `developer`, `mameparent`,
`publisher`, `search`. Taxonomy types (e.g., `region`, `year`, `lang`, `genre`, `series`)
have finite vocabularies per system and are always returned in full without truncation.

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
| systems     | string[] | No       | List of system IDs to restrict indexing to. Other system indexes will remain as is. |
| fuzzySystem | boolean  | No       | Enable fuzzy matching for system IDs in the `systems` array (e.g., `"snes"` matches `"SNES"`). |

An omitted or `null` value parameters key is also valid and will index every system.

**Selective Indexing Behavior:**
- When `systems` is provided with specific system IDs, only those systems will be reindexed
- The server will validate all provided system IDs and return an error if any are invalid
- If all systems are specified (equivalent to no restriction), a full database rebuild will be performed for optimal performance
- Selective indexing cannot be performed while database optimization is running
- Resume functionality will validate that the system configuration hasn't changed between indexing sessions

#### Result

Returns `null` on success. Indexing runs in the background after the response is sent. Track progress using [media.indexing](./notifications.md) notifications.

#### Examples

##### Full index request

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

##### Selective index request

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

##### Response (indexing was running)

```json
{
  "jsonrpc": "2.0",
  "id": "8f40e28e-7a5e-11ef-86dd-020304050607",
  "result": {
    "message": "Media indexing cancelled successfully"
  }
}
```

##### Response (no indexing running)

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

Returns an [ActiveMedia](#active-media-object) object if media is currently active, or `null` if no media is active.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "method": "media.active"
}
```

##### Response (no active media)

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": null
}
```

##### Response (media active)

```json
{
  "jsonrpc": "2.0",
  "id": "47f80537-7a5d-11ef-9c7b-020304050607",
  "result": {
    "started": "2024-09-24T17:49:42.938167429+08:00",
    "launcherId": "SNES",
    "systemId": "SNES",
    "systemName": "Super Nintendo Entertainment System",
    "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
    "mediaName": "Super Mario World",
    "zapScript": "@SNES/Super Mario World",
    "launcherControls": ["load_state", "save_state", "toggle_menu"]
  }
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

### media.history

Return paginated media play history.

#### Parameters

Optionally, an object:

| Key         | Type     | Required | Description                                                                                     |
| :---------- | :------- | :------- | :---------------------------------------------------------------------------------------------- |
| limit       | number   | No       | Maximum number of entries to return. Default is 25, maximum is 100.                              |
| cursor      | string   | No       | Cursor for pagination. Omit for first page, use `nextCursor` from previous response for subsequent pages. |
| systems     | string[] | No       | Filter to one or more system IDs (e.g., `["SNES", "NES"]`).                                     |
| fuzzySystem | boolean  | No       | Enable fuzzy matching for system IDs.                                                            |

#### Result

| Key        | Type                                                 | Required | Description                              |
| :--------- | :--------------------------------------------------- | :------- | :--------------------------------------- |
| entries    | [MediaHistoryEntry](#media-history-entry-object)[]   | Yes      | A list of media play history entries.    |
| pagination | [Pagination](#pagination-object)                     | No       | Pagination information for cursor-based navigation. Only present when entries are returned. |

##### Media history entry object

| Key        | Type   | Required | Description                                            |
| :--------- | :----- | :------- | :----------------------------------------------------- |
| systemId   | string | Yes      | ID of the system.                                      |
| systemName | string | Yes      | Display name of the system.                            |
| mediaName  | string | Yes      | Display name of the media.                             |
| mediaPath  | string | Yes      | Path to the media file.                                |
| launcherId | string | Yes      | ID of the launcher used.                               |
| startedAt  | string | Yes      | Timestamp when media started in RFC3339 format.        |
| endedAt    | string | No       | Timestamp when media stopped in RFC3339 format. Omitted if media is still active. |
| playTime   | number | Yes      | Duration of the play session in seconds.               |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5d-11ef-9c7b-020304050607",
  "method": "media.history",
  "params": {
    "limit": 10
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5d-11ef-9c7b-020304050607",
  "result": {
    "entries": [
      {
        "systemId": "SNES",
        "systemName": "Super Nintendo Entertainment System",
        "mediaName": "Super Mario World",
        "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
        "launcherId": "SNES",
        "startedAt": "2025-01-22T14:30:00Z",
        "endedAt": "2025-01-22T15:15:30Z",
        "playTime": 2730
      }
    ],
    "pagination": {
      "hasNextPage": false,
      "pageSize": 10
    }
  }
}
```

### media.history.top

Return aggregated media play history grouped by game, sorted by total play time descending. Useful for "most played" displays.

#### Parameters

Optionally, an object:

| Key         | Type     | Required | Description                                                                                     |
| :---------- | :------- | :------- | :---------------------------------------------------------------------------------------------- |
| limit       | number   | No       | Maximum number of entries to return. Default is 25, maximum is 100.                              |
| systems     | string[] | No       | Filter to one or more system IDs (e.g., `["SNES", "NES"]`).                                     |
| fuzzySystem | boolean  | No       | Enable fuzzy matching for system IDs.                                                            |
| since       | string   | No       | Only count sessions starting after this RFC3339 timestamp.                                       |

#### Result

| Key     | Type                                                         | Required | Description                              |
| :------ | :----------------------------------------------------------- | :------- | :--------------------------------------- |
| entries | [MediaHistoryTopEntry](#media-history-top-entry-object)[]    | Yes      | A ranked list of games by total play time. |

##### Media history top entry object

| Key           | Type   | Required | Description                                            |
| :------------ | :----- | :------- | :----------------------------------------------------- |
| systemId      | string | Yes      | ID of the system.                                      |
| systemName    | string | Yes      | Display name of the system.                            |
| mediaName     | string | Yes      | Display name of the media.                             |
| mediaPath     | string | Yes      | Path to the media file (from most recent session).     |
| totalPlayTime | number | Yes      | Total play time across all sessions in seconds.        |
| sessionCount  | number | Yes      | Number of play sessions.                               |
| lastPlayedAt  | string | Yes      | Timestamp of the most recent session in RFC3339 format. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-8b6e-12f0-ad8c-030405060708",
  "method": "media.history.top",
  "params": {
    "limit": 5,
    "systems": ["SNES"]
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-8b6e-12f0-ad8c-030405060708",
  "result": {
    "entries": [
      {
        "systemId": "SNES",
        "systemName": "Super Nintendo Entertainment System",
        "mediaName": "Super Mario World",
        "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
        "totalPlayTime": 7200,
        "sessionCount": 12,
        "lastPlayedAt": "2026-02-14T20:30:00Z"
      }
    ]
  }
}
```

### media.lookup

Resolve a game name and system to a media database match.

Given a system ID and game name, searches the media database for the best matching title. Uses fuzzy matching to handle minor differences in naming. Returns `null` for the match when no title is found or confidence is too low.

#### Parameters

An object:

| Key         | Type    | Required | Description                                                                            |
| :---------- | :------ | :------- | :------------------------------------------------------------------------------------- |
| system      | string  | Yes      | System ID to search within (e.g., `"SNES"`, `"Genesis"`).                             |
| name        | string  | Yes      | Game name to look up.                                                                   |
| fuzzySystem | boolean | No       | Enable fuzzy matching for the system ID (e.g., `"snes"` matches `"SNES"`).             |

#### Result

| Key   | Type                                         | Required | Description                                            |
| :---- | :------------------------------------------- | :------- | :----------------------------------------------------- |
| match | [MediaLookupMatch](#media-lookup-match-object) | No       | The best matching media entry, or `null` if no match found. |

##### Media lookup match object

| Key        | Type                         | Required | Description                                                                                 |
| :--------- | :--------------------------- | :------- | :------------------------------------------------------------------------------------------ |
| system     | [System](#system-object)     | Yes      | System the media was found in.                                                              |
| name       | string                       | Yes      | Display name of the matched media.                                                          |
| path       | string                       | Yes      | Path to the media file.                                                                     |
| zapScript  | string                       | Yes      | ZapScript command to launch this media item.                                                |
| tags       | [TagInfo](#taginfo-object)[] | Yes      | Array of tags associated with this media item.                                              |
| confidence | number                       | Yes      | Match confidence score from 0.0 to 1.0.                                                    |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5d-11ef-9c7b-020304050607",
  "method": "media.lookup",
  "params": {
    "system": "SNES",
    "name": "Super Mario World"
  }
}
```

##### Response (match found)

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5d-11ef-9c7b-020304050607",
  "result": {
    "match": {
      "system": {
        "id": "SNES",
        "name": "Super Nintendo Entertainment System",
        "category": "Console",
        "releaseDate": "1990-11-21",
        "manufacturer": "Nintendo"
      },
      "name": "Super Mario World",
      "path": "SNES/Super Mario World (USA).sfc",
      "zapScript": "@SNES/Super Mario World",
      "tags": [
        {
          "tag": "platformer",
          "type": "genre"
        },
        {
          "tag": "1990",
          "type": "year"
        }
      ],
      "confidence": 0.95
    }
  }
}
```

##### Response (no match)

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-7a5d-11ef-9c7b-020304050607",
  "result": {
    "match": null
  }
}
```

### media.control

Send a control action to the active media's launcher.

Requires active media with a launcher that supports control capabilities. The available control actions depend on the launcher. Use the `launcherControls` field from `media.active` or `media` to discover supported actions.

Control actions run in a restricted runtime that blocks media-launching and playlist commands. Utility commands like `input.keyboard`, `execute`, `delay` and `echo` are allowed. The `execute` command bypasses the `allow_execute` allowlist for control scripts defined in launcher configuration.

#### Parameters

An object:

| Key    | Type   | Required | Description                                                       |
| :----- | :----- | :------- | :---------------------------------------------------------------- |
| action | string | Yes      | The control action to execute (e.g., `"save_state"`, `"toggle_pause"`). |
| args   | object | No       | Optional key-value arguments for the control action. Values are strings. |

#### Result

Returns an empty object `{}` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "c3d4e5f6-7a5d-11ef-9c7b-020304050607",
  "method": "media.control",
  "params": {
    "action": "save_state"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "c3d4e5f6-7a5d-11ef-9c7b-020304050607",
  "result": {}
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
| enabled  | bool   | No       | Whether the connection is enabled. Defaults to true if omitted. |

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
    "readersScanMode": "tap",
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

### settings.auth.claim

Redeem a claim token against a remote auth server and store the resulting credentials in `auth.toml`.

This method performs trust discovery using the `.well-known/zaparoo` protocol. It first verifies that the claim URL's root domain supports auth (`auth: 1` in the well-known response), then redeems the claim token to obtain a bearer credential. If the root domain's well-known response includes a `trusted` list, each related domain is checked for bidirectional trust confirmation before extending the credential.

#### Parameters

An object:

| Key      | Type   | Required | Description                                                    |
| :------- | :----- | :------- | :------------------------------------------------------------- |
| claimUrl | string | Yes      | HTTPS URL of the claim endpoint to redeem the token against.   |
| token    | string | Yes      | The one-time claim token to redeem.                            |

#### Result

| Key     | Type     | Required | Description                                                        |
| :------ | :------- | :------- | :----------------------------------------------------------------- |
| domains | string[] | Yes      | List of domains the credential was stored for (root + any trusted). |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-auth-claim-example",
  "method": "settings.auth.claim",
  "params": {
    "claimUrl": "https://api.example.com/auth/claim",
    "token": "claim-token-abc123"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-auth-claim-example",
  "result": {
    "domains": [
      "https://api.example.com",
      "https://cdn.example.com"
    ]
  }
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

##### Response (reset state)

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

##### Response (active game with limits)

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

##### Response (cooldown state)

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

Returns an empty object `{}` on success.

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
  "result": {}
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
| id           | string   | Yes      | Device path or system identifier of the reader. Legacy field, prefer `readerId` for stable identification. |
| readerId     | string   | Yes      | Stable reader ID, deterministic across restarts. Format: `{driver}-{hash}`. |
| driver       | string   | Yes      | Driver type for the reader (e.g., `"pn532"`, `"acr122pcsc"`, `"file"`). |
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
        "id": "/dev/ttyUSB0",
        "readerId": "pn532-ujqixjv6",
        "driver": "pn532",
        "info": "PN532 (1-2.3.1)",
        "capabilities": ["read", "write"],
        "connected": true
      }
    ]
  }
}
```

### readers.write

Attempt to write given text to the first available write-capable reader, if possible.

#### Parameters

An object:

| Key      | Type   | Required | Description                                                                  |
| :------- | :----- | :------- | :--------------------------------------------------------------------------- |
| text     | string | Yes      | ZapScript to be written to the token.                                        |
| readerId | string | No       | ID of a specific reader to write to. If omitted, uses the first available write-capable reader. |

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

Optionally, an object:

| Key      | Type   | Required | Description                                                                    |
| :------- | :----- | :------- | :----------------------------------------------------------------------------- |
| readerId | string | No       | ID of a specific reader to cancel write on. If omitted, cancels on all readers. |

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
```

## Input

Direct platform input control for remote control use cases. These methods bypass the token pipeline entirely: no hooks, history, or sound effects are triggered.

The input macro format is identical to what goes after the `:` in a ZapScript `input.keyboard` or `input.gamepad` command on a token. Each character is a separate keypress, `{...}` groups are special keys/combos, and `\` is the escape character.

### input.keyboard

Press keyboard keys using the ZapScript input macro format.

#### Parameters

An object:

| Key  | Type   | Required | Description                                                                                                                                          |
| :--- | :----- | :------- | :--------------------------------------------------------------------------------------------------------------------------------------------------- |
| keys | string | Yes      | Input macro string. Each character is a keypress, `{...}` for special keys (e.g. `{enter}`, `{f9}`, `{ctrl+q}`). Same format as ZapScript on a token. |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "method": "input.keyboard",
  "params": {
    "keys": "abc{enter}"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "result": null
}
```

### input.gamepad

Press gamepad buttons using the ZapScript input macro format.

#### Parameters

An object:

| Key     | Type   | Required | Description                                                                                                                                                  |
| :------ | :----- | :------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| buttons | string | Yes      | Input macro string. Each character is a button press, `{...}` for named buttons (e.g. `{up}`, `{start}`, `{l1}`). Same format as ZapScript on a token. |

#### Result

Returns `null` on success.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-2345-6789-abcd-ef0123456789",
  "method": "input.gamepad",
  "params": {
    "buttons": "^^vv<><>BA{start}"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "b2c3d4e5-2345-6789-abcd-ef0123456789",
  "result": null
}
```

## Screenshot

### screenshot

Capture a screenshot of the current platform display. Returns the image as base64-encoded data and the path where it was saved on disk.

Currently supported on MiSTer only. Other platforms will return an error.

#### Parameters

None.

#### Result

| Key  | Type   | Required | Description                                  |
| :--- | :----- | :------- | :------------------------------------------- |
| path | string | Yes      | Path where the screenshot was saved on disk. |
| data | string | Yes      | Base64-encoded image data.                   |
| size | number | Yes      | Size of the image data in bytes.             |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "method": "screenshot"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "result": {
    "path": "/media/fat/screenshots/MiSTer_20260329_181500.png",
    "data": "iVBORw0KGgo...",
    "size": 245760
  }
}
```

## Updates

### update.check

Check if a newer version of Zaparoo Core is available. Returns version information and release notes. On development builds, always returns `updateAvailable: false`.

#### Parameters

None.

#### Result

| Key             | Type    | Required | Description                                        |
| :-------------- | :------ | :------- | :------------------------------------------------- |
| currentVersion  | string  | Yes      | The currently running version.                     |
| latestVersion   | string  | No       | The latest available version (if check succeeded). |
| updateAvailable | boolean | Yes      | Whether a newer version is available.              |
| releaseNotes    | string  | No       | Release notes for the latest version.              |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "method": "update.check"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "result": {
    "currentVersion": "2.9.1",
    "latestVersion": "2.10.0",
    "updateAvailable": true,
    "releaseNotes": "..."
  }
}
```

### update.apply

Download and apply the latest available update, then gracefully restart the service. The response is sent to the client before the restart occurs. Returns an error if media indexing is in progress or if running a development build.

#### Parameters

None.

#### Result

| Key             | Type   | Required | Description                    |
| :-------------- | :----- | :------- | :----------------------------- |
| previousVersion | string | Yes      | The version before the update. |
| newVersion      | string | Yes      | The version after the update.  |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "method": "update.apply"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-1234-5678-9abc-def012345678",
  "result": {
    "previousVersion": "2.9.1",
    "newVersion": "2.10.0"
  }
}
```
