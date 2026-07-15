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

| Key       | Type                                      | Required | Description                            |
| :-------- | :---------------------------------------- | :------- | :------------------------------------- |
| database  | [IndexingStatus](#indexing-status-object) | Yes      | Status of the media database.           |
| active    | [ActiveMedia](#active-media-object)[]     | Yes      | List of currently active media.         |
| playlists | [PlaylistState](#playlist-state-object)[] | No       | Currently active playlist slots.        |

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
| mediaId          | number   | No       | Opaque media database row ID for efficient follow-up `media.meta` and `media.image` requests. Omitted when the active path cannot be resolved in the current media database. |
| launcherId       | string   | Yes      | ID of the launcher.                        |
| systemId         | string   | Yes      | ID of the system.                          |
| systemName       | string   | Yes      | Display name of the system.                |
| mediaPath        | string   | Yes      | Path to the media file.                    |
| relativePath     | string   | No       | Launcher-relative convenience path, when it can be derived. Not a stable media identity. |
| positionMs       | number   | No       | Current playback position in milliseconds for native audio media. |
| durationMs       | number   | No       | Total playback duration in milliseconds for native audio media. |
| mediaName        | string   | Yes      | Display name of the media.                 |
| slot             | string   | No       | Media slot for the item. Omitted or `primary` is foreground media; `background` is background audio. |
| started          | string   | Yes      | Timestamp when media started in RFC3339 format. |
| zapScript        | string   | Yes      | ZapScript command to launch this media item. |
| launcherControls | string[] | No       | List of control action names supported by the active launcher. Only present if the launcher supports controls. See [media.control](#mediacontrol). |

##### Playlist state object

| Key     | Type   | Required | Description |
| :------ | :----- | :------- | :---------- |
| id      | string | Yes      | Playlist ID. |
| name    | string | Yes      | Playlist display name. |
| slot    | string | Yes      | Playlist slot, `primary` or `background`. |
| repeat  | string | Yes      | Repeat mode: `none`, `all`, or `one`. |
| items   | object[] | Yes    | Playlist items. |
| index   | number | Yes      | Zero-based current item index. |
| total   | number | Yes      | Total item count. |
| playing | boolean | Yes    | Whether playlist slot is playing. |

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
| mediaId   | number                   | No       | Opaque media database row ID for efficient follow-up `media.meta` and `media.image` requests.                |
| system    | [System](#system-object) | Yes      | System which the media has been indexed under.                                                              |
| name      | string                   | Yes      | A human-readable version of the result's filename without a file extension.                                 |
| path      | string                   | Yes      | Canonical indexed media path. Use with `system.id` for `media.meta` and `media.image`. |
| relativePath | string               | No       | Launcher-relative convenience path, when it can be derived. Not a stable media identity. |
| zapScript | string                   | Yes      | ZapScript command to launch this media item. Includes the disambiguating tags inline (e.g. `@Arcade/X-Men Vs. Street Fighter (region:eu) (builddate:1996-10-04)`) so the written command resolves back to this specific variant. |
| tags      | [TagInfo](#taginfo-object)[] | Yes      | Array of tags associated with this media item.                                               |
| disambiguatingTags | [TagInfo](#taginfo-object)[] | No | Subset of `tags` whose values differ across same-named siblings of this title, ordered by display importance. Omitted when the title has nothing to disambiguate. Clients can render these to tell variants apart. |

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
        "mediaId": 123,
        "name": "240p Test Suite (PD) v0.03 tepples",
        "path": "/media/fat/games/Gameboy/240p Test Suite (PD) v0.03 tepples.gb",
        "relativePath": "Gameboy/240p Test Suite (PD) v0.03 tepples.gb",
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
        "mediaId": 456,
        "name": "Super Mario Bros.",
        "path": "/media/fat/games/NES/Super Mario Bros.nes",
        "relativePath": "NES/Super Mario Bros.nes",
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
| sort       | string | No       | Sort order. One of: `name-asc` (default), `name-desc`, `filename-asc`, `filename-desc`. Name sorting is prefix-aware for detected ranked/date collection folders. The `filename` variants sort by full file path. |

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
| mediaId      | number   | No       | Opaque media database row ID. Present on `media` entries, and on zip-as-directory platform `directory` entries whose direct contents collapse to one logical launch target, for efficient follow-up `media.meta` and `media.image` requests. |
| name         | string   | Yes      | Display name of the entry.                                                                       |
| path         | string   | Yes      | Full path to the entry.                                                                          |
| type         | string   | Yes      | Entry type: `root`, `directory`, or `media`.                                                     |
| fileCount    | number   | No       | Number of files in this directory. Present on `root` and `directory` entries, except a `root` entry whose exact count could not be computed in time (known non-empty, count omitted). |
| group        | string   | No       | Launcher group name. Present on virtual scheme `root` entries.                                   |
| systemId     | string   | No       | System ID for the media or single-system filtered route (e.g. `SNES`). Present on `media` entries and filtered `root` entries when exactly one system applies. |
| systemIds    | string[] | No       | System IDs represented by a filtered `root` or `directory` entry.                                |
| zapScript    | string   | No       | ZapScript command to launch this media. Present on `media` entries and logical single-game container `directory` entries on zip-as-directory platforms. |
| relativePath | string   | No       | Relative path from root directory. Present on `media` entries and logical single-game container `directory` entries on zip-as-directory platforms. |
| tags         | object[] | No       | Tags attached to the media. Each object has `tag` (string) and `type` (string). Present on `media` entries and logical single-game container `directory` entries on zip-as-directory platforms. |
| disambiguatingTags | object[] | No | Subset of `tags` whose values differ across same-named siblings of this title, ordered by display importance. Same object shape as `tags`. Omitted when the title has nothing to disambiguate. |

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
        "mediaId": 42,
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
        "mediaId": 43,
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

### media.browse.index

Return the ordered first-character "jump to letter" buckets for a browse scope. Each bucket carries a count and a ready-to-use cursor that seeks `media.browse` to the start of that bucket, so a single round trip gives a client everything it needs to draw a section rail _and_ jump into the full ordered list. This avoids paging from the top to reach a distant section, which matters on constrained clients (e.g. MiSTer).

The scope parameters mirror `media.browse` so the index describes the exact list `media.browse` would return for the same scope. The per-bucket `cursor` is an ordinary browse cursor: pass it to `media.browse` with the same `path`/`systems`/`sort` to get a normal page that begins at the bucket and continues into the next bucket as the user scrolls.

#### Parameters

All parameters are optional.

| Key         | Type     | Required | Description                                                                                       |
| :---------- | :------- | :------- | :------------------------------------------------------------------------------------------------ |
| path        | string   | No       | Directory or virtual scheme to index, same as `media.browse`. Omit or set empty for a root listing (no rail applies). |
| systems     | string[] | No       | Case-sensitive system IDs to scope the index to, same as `media.browse`.                          |
| fuzzySystem | boolean  | No       | Enable fuzzy matching for system IDs in `systems`.                                                |
| sort        | string   | No       | Sort order, must match the `media.browse` sort the rail is for. One of `name-asc` (default), `name-desc`, `filename-asc`, `filename-desc`. |

#### Result

| Key        | Type                                          | Required | Description                                                                 |
| :--------- | :-------------------------------------------- | :------- | :-------------------------------------------------------------------------- |
| scheme     | string                                        | Yes      | Collation used to derive the buckets. `latin` for first-character bucketing; `none` when no rail applies (a root listing, or a directory whose effective sort is not alphabetical, e.g. a ranked/date-prefixed collection folder), in which case `groups` is empty. |
| totalFiles | number                                        | Yes      | Total media files in the scope.                                             |
| groups     | [BrowseIndexGroup](#browse-index-group-object)[] | Yes   | Only non-empty buckets, ordered to match `sort`.                            |

##### Browse index group object

| Key    | Type   | Required | Description                                                                                       |
| :----- | :----- | :------- | :------------------------------------------------------------------------------------------------ |
| key    | string | Yes      | Stable bucket identifier (`A`–`Z`, `0-9`, `#`). Treat as opaque.                                  |
| label  | string | Yes      | Display text for the bucket. Equal to `key` for the `latin` scheme.                               |
| count  | number | Yes      | Number of media files in the bucket.                                                              |
| cursor | string | Yes      | Opaque `media.browse` cursor positioned just before the bucket's first row. Empty string for the bucket that begins the list (call `media.browse` with no cursor for the first page). |
| offset | number | Yes      | 0-based position of the bucket's first item among the scope's media files, taken from its row number in the same ordered listing `media.browse` pages through (so it cannot drift from the browse order). Excludes any directory entries the listing shows before files; a client that jumps to a position in the full list adds its own leading-directory count. Use this to jump to the bucket's position rather than reloading from `cursor`. |

Clients should render `groups` exactly as received, in order, without assuming a particular alphabet: `scheme` and `key` are opaque so a future locale-aware scheme (e.g. pinyin/kana/hangul buckets) requires no client change.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "media.browse.index",
  "params": {
    "path": "/roms/SNES",
    "sort": "name-asc"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "scheme": "latin",
    "totalFiles": 150,
    "groups": [
      { "key": "#", "label": "#", "count": 3, "cursor": "", "offset": 0 },
      { "key": "0-9", "label": "0-9", "count": 7, "cursor": "eyJzb3J0VmFsdWUiOiIjV29sZiIsImxhc3RJZCI6MTAyfQ==", "offset": 3 },
      { "key": "A", "label": "A", "count": 12, "cursor": "eyJzb3J0VmFsdWUiOiI5IExpdmVzIiwibGFzdElkIjoxMTV9", "offset": 10 }
    ]
  }
}
```

To jump to "A", the client calls `media.browse` with that group's `cursor` and the same `path`/`sort`; the returned page begins at the first "A" title and continues into "B" as the user keeps scrolling.

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
`publisher`, `search`. Taxonomy types (e.g., `region`, `year`, `lang`, `gamegenre`, `gamefamily`)
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
        "type": "gamefamily",
        "tag": "Mario Bros"
      },
      {
        "type": "gamefamily",
        "tag": "Super Mario"
      }
    ]
  }
}
```

### media.tags.update

Add or remove user tags for an indexed media item.

The initial mutable tag is `user:favorite`. It appears in normal media tag results and can be queried with `media.search` tag filters such as `user:favorite`, `-user:favorite`, and `~user:favorite`.

#### Parameters

| Key     | Type     | Required | Description                                               |
| :------ | :------- | :------- | :-------------------------------------------------------- |
| mediaId | number   | No       | Media DBID to update. Cannot be mixed with system/path.   |
| system  | string   | No       | System ID for path-based lookup. Required when using path. |
| path    | string   | No       | Media path for path-based lookup. Required with system.    |
| add     | string[] | No       | Tags to add. Currently only `user:favorite` is mutable.    |
| remove  | string[] | No       | Tags to remove. Currently only `user:favorite` is mutable. |

Either `mediaId` or `system` plus `path` is required. At least one of `add` or `remove` is required. Search operators (`+`, `-`, `~`) are not valid in mutation requests.

#### Result

| Key  | Type                         | Required | Description                          |
| :--- | :--------------------------- | :------- | :----------------------------------- |
| tags | [TagInfo](#taginfo-object)[] | Yes      | Effective tags for the media item.   |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a1b2c3d4-7a5d-11ef-9c7b-020304050607",
  "method": "media.tags.update",
  "params": {
    "mediaId": 42,
    "add": ["user:favorite"]
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
        "type": "user",
        "tag": "favorite"
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
| rebuild     | boolean  | No       | Discard the media database entirely and index from scratch ("fresh start"). Scraped metadata is lost and must be re-scraped; favourites and launcher overrides are preserved (they live in the user database and are re-applied after indexing). Cannot be combined with `systems`. |

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

| Key  | Type   | Required | Description |
| :--- | :----- | :------- | :---------- |
| slot | string | No       | Media slot to query. Use `primary` or `background`. Defaults to `primary`. |

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
    "mediaId": 42,
    "started": "2024-09-24T17:49:42.938167429+08:00",
    "launcherId": "SNES",
    "systemId": "SNES",
    "systemName": "Super Nintendo Entertainment System",
    "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
    "relativePath": "snes/Super Mario World (USA).sfc",
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

### media.history.latest

Return the most recent played media entry from the user database only. This is intended for startup paths that need the last played game as quickly as possible, without media database enrichment.

This method does not return tags, metadata, media IDs, relative paths, pagination, end time, or play time.

#### Parameters

None. Empty params may be omitted or sent as `{}`.

#### Result

| Key   | Type                                                         | Required | Description                                                   |
| :---- | :----------------------------------------------------------- | :------- | :------------------------------------------------------------ |
| entry | [MediaHistoryLatestEntry](#media-history-latest-entry-object) | Yes      | Most recent media play history entry, or `null` when none exists. |

##### Media history latest entry object

| Key        | Type   | Required | Description                                     |
| :--------- | :----- | :------- | :---------------------------------------------- |
| systemId   | string | Yes      | ID of the system.                               |
| systemName | string | Yes      | Display name of the system from the history row. |
| mediaName  | string | Yes      | Display name of the media from the history row. |
| mediaPath  | string | Yes      | Path to the media file from the history row.    |
| launcherId | string | Yes      | ID of the launcher used.                        |
| startedAt  | string | Yes      | Timestamp when media started in RFC3339 format. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "9f2c6a52-7a5d-11ef-9c7b-020304050607",
  "method": "media.history.latest"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "9f2c6a52-7a5d-11ef-9c7b-020304050607",
  "result": {
    "entry": {
      "systemId": "SNES",
      "systemName": "Super Nintendo Entertainment System",
      "mediaName": "Super Mario World",
      "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
      "launcherId": "SNES",
      "startedAt": "2025-01-22T14:30:00Z"
    }
  }
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
| mediaId    | number | No       | Opaque media database row ID for efficient follow-up `media.meta` and `media.image` requests. Omitted when the history path cannot be resolved in the current media database. |
| systemId   | string | Yes      | ID of the system.                                      |
| systemName | string | Yes      | Display name of the system.                            |
| mediaName  | string | Yes      | Display name of the media.                             |
| mediaPath  | string | Yes      | Path to the media file.                                |
| relativePath | string | No     | Launcher-relative convenience path, when it can be derived. Not a stable media identity. |
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
        "mediaId": 42,
        "systemId": "SNES",
        "systemName": "Super Nintendo Entertainment System",
        "mediaName": "Super Mario World",
        "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
        "relativePath": "snes/Super Mario World (USA).sfc",
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
| mediaId       | number | No       | Opaque media database row ID for efficient follow-up `media.meta` and `media.image` requests. Omitted when the history path cannot be resolved in the current media database. |
| systemId      | string | Yes      | ID of the system.                                      |
| systemName    | string | Yes      | Display name of the system.                            |
| mediaName     | string | Yes      | Display name of the media.                             |
| mediaPath     | string | Yes      | Path to the media file (from most recent session).     |
| relativePath  | string | No       | Launcher-relative convenience path, when it can be derived. Not a stable media identity. |
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
        "mediaId": 42,
        "systemId": "SNES",
        "systemName": "Super Nintendo Entertainment System",
        "mediaName": "Super Mario World",
        "mediaPath": "/roms/snes/Super Mario World (USA).sfc",
        "relativePath": "snes/Super Mario World (USA).sfc",
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
| mediaId    | number                       | No       | Opaque media database row ID for efficient follow-up `media.meta` and `media.image` requests. |
| system     | [System](#system-object)     | Yes      | System the media was found in.                                                              |
| name       | string                       | Yes      | Display name of the matched media.                                                          |
| path       | string                       | Yes      | Path to the media file.                                                                     |
| relativePath | string                    | No       | Launcher-relative convenience path, when it can be derived. Not a stable media identity.   |
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
      "mediaId": 42,
      "system": {
        "id": "SNES",
        "name": "Super Nintendo Entertainment System",
        "category": "Console",
        "releaseDate": "1990-11-21",
        "manufacturer": "Nintendo"
      },
      "name": "Super Mario World",
      "path": "/roms/snes/Super Mario World (USA).sfc",
      "relativePath": "SNES/Super Mario World (USA).sfc",
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

### media.meta

Return the full metadata graph for one indexed media row, including its title, system, tags, and scraped properties.

Use this when a client has a search, browse, or lookup result and needs all metadata attached to that row. Identify media by the result's `mediaId` when available, or by `system.id` and canonical `path`. Launcher-relative paths in the `system/path` shape are accepted as a compatibility fallback when they resolve to exactly one indexed media row. Properties are separated by scope: `media.properties` applies to the specific ROM/file row, and `media.title.properties` applies to the shared title.

#### Parameters

An object:

| Key    | Type   | Required | Description                         |
| :----- | :----- | :------- | :---------------------------------- |
| mediaId | number | No      | Opaque media database row ID from search, browse, or lookup. Cannot be mixed with `system`/`path`. |
| system | string | No       | System ID for the media row. Required when `mediaId` is omitted. |
| path   | string | No       | Canonical indexed media path. Required when `mediaId` is omitted. |
| items  | object[] | No     | Batch request items. Each item uses either `mediaId` or `system`/`path`. Maximum 100 items. Cannot be mixed with top-level media ref fields. |

Single requests return the existing single `media` response shape. Batch requests return `{ "items": [...] }` in input order. Each batch item contains either `media` or `error`, so one missing media row does not fail the whole batch.

#### Result

| Key   | Type                                  | Required | Description                  |
| :---- | :------------------------------------ | :------- | :--------------------------- |
| media | [MediaMeta](#media-meta-object)       | Yes      | Metadata for the media row.  |

##### Media meta object

| Key        | Type                                    | Required | Description                                           |
| :--------- | :-------------------------------------- | :------- | :---------------------------------------------------- |
| path       | string                                  | Yes      | Media file path.                                      |
| parentDir  | string                                  | Yes      | Parent directory stored for the media row.            |
| isMissing  | boolean                                 | Yes      | Whether the indexed file is currently missing.        |
| tags       | [TagInfo](#taginfo-object)[]            | Yes      | ROM-level tags for this media row.                    |
| properties | object                                  | Yes      | ROM-level properties keyed by canonical type tag.     |
| launcherOverride | string                            | No       | Launcher ID stored for this media row, mirrored from `property:launcher-override` in `properties`. When present, Core uses it for title, search, path, random, and history launches unless ZapScript includes an explicit `launcher` argument. |
| title      | [MediaMetaTitle](#media-meta-title-object) | Yes   | Shared title metadata for this media row.             |

##### Media meta title object

| Key           | Type                         | Required | Description                                      |
| :------------ | :--------------------------- | :------- | :----------------------------------------------- |
| slug          | string                       | Yes      | Primary normalized title slug.                   |
| secondarySlug | string                       | No       | Secondary title slug, when available.            |
| name          | string                       | Yes      | Display title.                                   |
| slugLength    | number                       | Yes      | Character length of the primary slug.            |
| slugWordCount | number                       | Yes      | Word count of the primary slug.                  |
| system        | object                       | Yes      | Stored system object with `id` and `name`.       |
| tags          | [TagInfo](#taginfo-object)[] | Yes      | Title-level tags shared by matching media rows.  |
| properties    | object                       | Yes      | Title-level properties keyed by canonical type tag. |

##### Media meta property object

| Key         | Type   | Required | Description                                                            |
| :---------- | :----- | :------- | :--------------------------------------------------------------------- |
| text        | string | Yes      | Text value or source path for the property.                            |
| contentType | string | Yes      | MIME type for binary-backed properties, empty for text-only values.    |
| extension   | string | No       | File extension without a dot, derived from MIME type or source path.   |
| blobSize    | number | No       | Size in bytes for binary-backed properties. |

Binary property data is not returned by `media.meta`. Use `media.image` to fetch image bytes.
Property keys are canonical type tags such as `property:description`, `property:image-image`, or `property:manual`.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "d4e5f6a7-7a5d-11ef-9c7b-020304050607",
  "method": "media.meta",
  "params": {
    "system": "SNES",
    "path": "/roms/snes/Super Mario World.sfc"
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "d4e5f6a7-7a5d-11ef-9c7b-020304050607",
  "result": {
    "media": {
      "path": "/roms/snes/Super Mario World.sfc",
      "parentDir": "/roms/snes",
      "isMissing": false,
      "tags": [
        {"type": "region", "tag": "usa"}
      ],
      "properties": {
        "property:launcher-override": {
          "text": "RetroArch",
          "contentType": ""
        }
      },
      "launcherOverride": "RetroArch",
      "title": {
        "slug": "super mario world",
        "name": "Super Mario World",
        "slugLength": 17,
        "slugWordCount": 3,
        "system": {
          "id": "SNES",
          "name": "Super Nintendo Entertainment System"
        },
        "tags": [
          {"type": "developer", "tag": "Nintendo"},
          {"type": "gamegenre", "tag": "platformer"}
        ],
        "properties": {
          "property:description": {
            "text": "Mario's dinosaur friend Yoshi makes his debut.",
            "contentType": ""
          }
        }
      }
    }
  }
}
```

##### Batch Request

```json
{
  "jsonrpc": "2.0",
  "id": "d4e5f6a7-7a5d-11ef-9c7b-020304050608",
  "method": "media.meta",
  "params": {
    "items": [
      {"mediaId": 42},
      {"system": "SNES", "path": "/roms/snes/Super Metroid.sfc"}
    ]
  }
}
```

### media.meta.update

Update writable metadata fields for one indexed media row, then return the same response shape as [`media.meta`](#mediameta).

Use this to store a per-media launcher override. Core validates the launcher exists and supports the media row's system before saving it. Set `launcherOverride` to `null` to clear the override.

Launcher selection order is:

1. Explicit `launcher` advanced argument in ZapScript.
2. Per-media `launcherOverride` stored with `media.meta.update`.
3. System default launcher from configuration.
4. Normal launcher matching.

#### Parameters

An object identifying the media row by `mediaId` or by `system` and canonical `path`.

| Key     | Type   | Required | Description |
| :------ | :----- | :------- | :---------- |
| mediaId | number | No       | Opaque media database row ID from search, browse, or lookup. Cannot be mixed with `system`/`path`. |
| system  | string | No       | System ID for the media row. Required when `mediaId` is omitted. |
| path    | string | No       | Canonical indexed media path. Required when `mediaId` is omitted. |
| media   | object | Yes      | Patch object. Currently supports only `launcherOverride`. |

##### Media patch object

| Key              | Type        | Required | Description |
| :--------------- | :---------- | :------- | :---------- |
| launcherOverride | string\|null | Yes      | Launcher ID to use for this media row, matched case-insensitively and stored with canonical casing. Use `null` to clear it. Empty strings are rejected. |

#### Result

| Key   | Type                            | Required | Description                 |
| :---- | :------------------------------ | :------- | :-------------------------- |
| media | [MediaMeta](#media-meta-object) | Yes      | Updated metadata for row.   |

#### Example

##### Set override

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "media.meta.update",
  "params": {
    "mediaId": 42,
    "media": {
      "launcherOverride": "RetroArch"
    }
  }
}
```

##### Clear override

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "media.meta.update",
  "params": {
    "system": "SNES",
    "path": "/roms/snes/Super Mario World.sfc",
    "media": {
      "launcherOverride": null
    }
  }
}
```

### media.image

Return the best matching image for one indexed media row as base64-encoded data.

`media.image` checks the requested image types in order. For each type it tries media-level properties first, then title-level properties. If a stored file path no longer exists, the stale property is removed and lookup continues.

#### Parameters

An object identifying the media row by `mediaId` or `(system, path)`. Canonical indexed paths are preferred. Launcher-relative paths in the `system/path` shape are accepted as a compatibility fallback when they resolve to exactly one indexed media row.

| Key        | Type     | Required | Description                                                                 |
| :--------- | :------- | :------- | :-------------------------------------------------------------------------- |
| mediaId    | number   | No       | Opaque media database row ID from search, browse, or lookup. Cannot be mixed with `system`/`path`. |
| system     | string   | No       | System ID. Required when `mediaId` is omitted.                              |
| path       | string   | No       | Canonical indexed media path. Required when `mediaId` is omitted.            |
| imageTypes | string[] | No       | Image type preference order. Defaults to `image`, `thumbnail`, `boxart`, `boxart3d`, `screenshot`, `wheel`, `titleshot`, `map`, `marquee`, `fanart`. |
| maxSize    | number   | No       | Longest-edge size hint in pixels. When set, the server resizes the image to fit a `maxSize`×`maxSize` box and caches the result; omit it for the full-size image. |

Supported image type values are `image`, `thumbnail`, `boxart`, `boxart3d`, `screenshot`, `wheel`, `titleshot`, `map`, `marquee`, and `fanart`. They resolve to canonical property tags such as `property:image-image` and `property:image-boxart`.

Resizing is intended for grid and preview views where transferring and holding full-size art is expensive. `maxSize` is snapped up to the nearest of a small set of standard tiers (`32`, `64`, `128`, `256`, `512`, `768`) server-side. The returned image is **never larger than the snapped tier and never larger than the source** — when the source already fits the tier it is returned at its native dimensions, so the result may still be larger than the exact `maxSize` you asked for. Request your true display size (logical size × pixel ratio) and downscale to the final size on the client. The snapped tiers bound how many resized variants are cached per image. Output is re-encoded as WebP (lossy, alpha preserved) regardless of source format — including when the source already fits the box, so even a near-native request still gets the smaller WebP — and cached on disk so repeat requests are cheap. The original bytes are kept only when WebP would not shrink them (already-compact sources), when `maxSize` is omitted/non-positive (full size), or when the source cannot be decoded.

#### Result

| Key         | Type   | Required | Description                                  |
| :---------- | :----- | :------- | :------------------------------------------- |
| contentType | string | Yes      | MIME type of the returned image data.        |
| extension   | string | No       | File extension without a dot, derived from MIME type or source path. |
| data        | string | Yes      | Base64-encoded image bytes.                  |
| typeTag     | string | Yes      | Canonical property tag that matched.         |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "e5f6a7b8-7a5d-11ef-9c7b-020304050607",
  "method": "media.image",
  "params": {
    "system": "SNES",
    "path": "/roms/snes/Super Mario World.sfc",
    "imageTypes": ["boxart", "image"],
    "maxSize": 512
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "e5f6a7b8-7a5d-11ef-9c7b-020304050607",
  "result": {
    "contentType": "image/webp",
    "extension": "webp",
    "data": "UklGRiQAAABXRUJQVlA4...",
    "typeTag": "property:image-boxart"
  }
}
```

### scrapers

List all registered metadata scrapers.

#### Parameters

None.

#### Result

| Key      | Type                                 | Required | Description                         |
| :------- | :----------------------------------- | :------- | :---------------------------------- |
| scrapers | [ScraperInfo](#scraper-info-object)[] | Yes      | Registered scraper implementations. |

##### Scraper info object

| Key              | Type     | Required | Description                                                              |
| :--------------- | :------- | :------- | :----------------------------------------------------------------------- |
| id               | string   | Yes      | Stable scraper ID used by `media.scrape`.                                |
| name             | string   | Yes      | Human-readable scraper name.                                             |
| supportedSystems | string[] | Yes      | Supported system IDs. Empty means the scraper can run against all systems. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "f6a7b8c9-7a5d-11ef-9c7b-020304050607",
  "method": "scrapers"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "f6a7b8c9-7a5d-11ef-9c7b-020304050607",
  "result": {
    "scrapers": [
      {
        "id": "gamelist.xml",
        "name": "gamelist.xml",
        "supportedSystems": []
      }
    ]
  }
}
```

### media.scrape

Start a metadata scraper run in the background.

Scraping enriches existing MediaDB records only. It does not create media rows; run `media.generate` first so the filesystem scanner has indexed the library. Scraping and media indexing are mutually exclusive, and only one scraper can run at a time.

Progress is reported with [media.scraping](./notifications.md#mediascraping) notifications and can be queried with `media.scrape.status`. Scraping pauses while media is running and resumes automatically when playback stops.

#### Parameters

An object:

| Key       | Type     | Required | Description                                                                 |
| :-------- | :------- | :------- | :-------------------------------------------------------------------------- |
| scraperId | string   | Yes      | Scraper ID from the `scrapers` method, for example `gamelist.xml`.          |
| systems   | string[] | No       | System IDs to scrape. Omit or pass an empty array to scrape all eligible systems. |
| force     | boolean  | No       | Re-scrape records that already have this scraper's sentinel tag. Default is false. |

#### Result

Returns `null` on success. The scraper continues after the response is sent.

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "a7b8c9d0-7a5d-11ef-9c7b-020304050607",
  "method": "media.scrape",
  "params": {
    "scraperId": "gamelist.xml",
    "systems": ["SNES", "NES"],
    "force": false
  }
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "a7b8c9d0-7a5d-11ef-9c7b-020304050607",
  "result": null
}
```

### media.scrape.status

Return the latest known metadata scraper status.

This method behaves like `media` does for indexing status: clients can query the current scrape snapshot after opening a UI, then continue listening for `media.scraping` notifications. If no scrape has run since startup, the result is idle with `scraping: false`, `done: false`, and `state: "idle"`. Existing flat counter fields remain for compatibility; new UIs should prefer `currentSystem` for per-system progress and `totalSteps`/`currentStep`/`currentStepDisplay` for whole-run progress.

#### Parameters

None.

#### Result

| Key       | Type    | Required | Description                                                |
| :-------- | :------ | :------- | :--------------------------------------------------------- |
| scraperId | string  | No       | Scraper ID for the latest or active run.                   |
| systemId  | string  | No       | System currently being processed, when known.              |
| processed | integer | Yes      | Number of records processed.                               |
| total     | integer | Yes      | Total records expected for the current scrape, when known. |
| matched   | integer | Yes      | Number of records matched and enriched.                    |
| skipped   | integer | Yes      | Number of records skipped.                                 |
| totalScraped | integer | Yes  | Number of media records already marked scraped.             |
| scraping  | boolean | Yes      | Whether a scrape is currently running.                     |
| done      | boolean | Yes      | Whether the latest scrape reached a terminal state.        |
| paused    | boolean | Yes      | Whether the active scrape is paused because media is running or until resumed. |
| state     | string  | No       | Explicit lifecycle state: `idle`, `running`, `paused`, `completed`, `cancelled`, or `failed`. |
| error     | string  | No       | Fatal scrape error on failed terminal updates.             |
| totalSteps | integer | No      | Total systems in the scrape run, when known.               |
| currentStep | integer | No     | 1-based current system step, when known.                   |
| currentStepDisplay | string | No | Display name for the current system step, falling back to system ID. |
| currentSystem | object | No    | Per-system progress object with `systemId`, `systemName`, `processed`, `total`, `matched`, and `skipped`. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "b8c9d0e1-7a5d-11ef-9c7b-020304050607",
  "method": "media.scrape.status"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "b8c9d0e1-7a5d-11ef-9c7b-020304050607",
  "result": {
    "scraperId": "gamelist.xml",
    "systemId": "snes",
    "processed": 42,
    "total": 100,
    "matched": 38,
    "skipped": 4,
    "totalScraped": 1200,
    "scraping": true,
    "done": false,
    "paused": false,
    "state": "running",
    "totalSteps": 2,
    "currentStep": 1,
    "currentStepDisplay": "Super Nintendo Entertainment System",
    "currentSystem": {
      "systemId": "snes",
      "systemName": "Super Nintendo Entertainment System",
      "processed": 42,
      "total": 100,
      "matched": 38,
      "skipped": 4
    }
  }
}
```

### media.scrape.cancel

Cancel the currently running metadata scraper operation.

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
  "id": "b8c9d0e1-7a5d-11ef-9c7b-020304050607",
  "method": "media.scrape.cancel"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "b8c9d0e1-7a5d-11ef-9c7b-020304050607",
  "result": {
    "message": "scraping cancelled"
  }
}
```

### media.scrape.resume

Resume a paused metadata scraper operation.

Scraping normally resumes automatically when playback stops. This method mirrors `media.generate.resume` and lets a local client force the active scrape to continue while the pauser is currently paused.

#### Parameters

None.

#### Result

| Key     | Type   | Required | Description                    |
| :------ | :----- | :------- | :----------------------------- |
| message | string | Yes      | Status message about resuming. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "c9d0e1f2-7a5d-11ef-9c7b-020304050607",
  "method": "media.scrape.resume"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "c9d0e1f2-7a5d-11ef-9c7b-020304050607",
  "result": {
    "message": "Media scraping resumed"
  }
}
```

### media.clean.orphans

Delete media rows marked missing and remove orphaned related data.

This is intended for cleanup after files have been removed from disk and the media database has been refreshed. It removes missing `Media` rows, their tags and properties, and any titles that no longer have media rows. It does not run `VACUUM`; SQLite will reuse freed pages.

#### Parameters

None.

#### Result

| Key     | Type   | Required | Description                       |
| :------ | :----- | :------- | :-------------------------------- |
| deleted | number | Yes      | Number of missing media rows removed. |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "c9d0e1f2-7a5d-11ef-9c7b-020304050607",
  "method": "media.clean.orphans"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "c9d0e1f2-7a5d-11ef-9c7b-020304050607",
  "result": {
    "deleted": 12
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
| slot   | string | No       | Target media slot. Omit for primary media; use `"background"` to control background audio. |
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

##### Background audio example

Native audio supports `toggle_pause`, `pause`, `resume`, `stop`, `fast_forward`, and `rewind` controls on the `background` slot. `fast_forward` and `rewind` accept an optional `seconds` argument; default is 10 seconds.

```json
{
  "jsonrpc": "2.0",
  "id": "d4e5f6a7-7a5d-11ef-9c7b-020304050607",
  "method": "media.control",
  "params": {
    "action": "fast_forward",
    "slot": "background",
    "args": {
      "seconds": "30"
    }
  }
}
```

### systems

List systems currently indexed or supported by an available launcher on the running platform. Virtual systems are also included.

Set `all` to include every system represented by the running platform's launcher definitions, even when its runtime dependency is currently unavailable. This is useful when selecting a specific system for its first media index.

#### Parameters

| Key | Type    | Required | Description                                                                                     |
| :-- | :------ | :------- | :---------------------------------------------------------------------------------------------- |
| all | boolean | No       | Include systems with unavailable launchers. Defaults to `false`. Indexed systems remain listed. |

#### Result

| Key     | Type                       | Required | Description                                                        |
| :------ | :------------------------- | :------- | :----------------------------------------------------------------- |
| systems | [System](#system-object)[] | Yes      | Indexed, available, and optionally unavailable platform systems.   |

See [System object](#system-object).

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "dbd312f3-7a5f-11ef-8f29-020304050607",
  "method": "systems",
  "params": {
    "all": true
  }
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
| encryption                | boolean                                   | Yes      | Whether paired encryption is required for remote WebSocket connections. Localhost remains exempt. |
| readersConnect            | [ReaderConnection](#reader-connection-object)[] | Yes      | List of manually configured reader connections.                 |
| systemDefaults            | [SystemDefault](#system-default-object)[] | Yes      | Per-system overrides for default launcher and exit ZapScript.   |
| profilesRequireForLaunch  | boolean                                   | Yes      | Whether media launches are blocked while no personal profile is active. |
| profilesSwapData          | boolean                                   | Yes      | Whether profile switches also swap profile-scoped data (saves, save states) on supported platforms. Defaults to true. |

##### Reader connection object

| Key      | Type   | Required | Description                                      |
| :------- | :----- | :------- | :----------------------------------------------- |
| driver   | string | Yes      | Reader driver type (e.g., "pn532uart", "acr122pcsc"). |
| path     | string | Yes      | Path or address for the reader connection.       |
| idSource | string | No       | Source for the reader ID.                        |
| enabled  | bool   | No       | Whether the connection is enabled. Defaults to true if omitted. |

##### System default object

| Key        | Type   | Required | Description                                                                                       |
| :--------- | :----- | :------- | :------------------------------------------------------------------------------------------------ |
| system     | string | Yes      | System ID this default applies to. Accepts canonical IDs and aliases.                             |
| launcher   | string | No       | Launcher ID or group name to use for this system. Empty means no override.                        |
| beforeExit | string | No       | ZapScript to run when a media instance for this system is exiting (before the new launch starts). |

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
    "encryption": false,
    "readersConnect": [],
    "systemDefaults": [
      {
        "system": "Genesis",
        "launcher": "retroarch"
      }
    ]
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
| encryption                | boolean                                   | No       | Require paired encryption for remote WebSocket connections. This setting can only be changed from localhost. |
| readersConnect            | [ReaderConnection](#reader-connection-object)[] | No       | List of manually configured reader connections.                 |
| systemDefaults            | [SystemDefault](#system-default-object)[] | No       | Replace the full list of per-system launcher/exit-script overrides. Each `launcher` value, if non-empty, must match a known launcher ID or group (case-insensitive). |
| profilesRequireForLaunch  | boolean                                   | No       | Whether media launches are blocked while no personal profile is active. |
| profilesSwapData          | boolean                                   | No       | Whether profile switches also swap profile-scoped data. Turning it off converges data back to the shared state immediately. |

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

## Profiles

Profiles are lightweight runtime identities: named buckets of preferences, limits, and profile-owned data. One profile is active per device at a time, switched via the API or by scanning an NFC card containing the profile's switch ID (`**profile:<switchId>`). Profile roles (`admin` or `member`) are separate from paired-client roles: profile roles identify who may authorize local household management, while client roles describe which remote device may call privileged APIs.

When no personal profile is active the device is on the implicit **shared profile** — the device as it behaves when nobody is signed in. The shared profile's playtime limits are the global config limits, its history is unattributed, and it owns everything the device did before profiles existed. Deactivating means switching to the shared profile. To stop the shared profile launching media (parking the device until someone identifies themselves), enable the `profilesRequireForLaunch` setting (see [settings](#settings)).

A profile's **switch ID is a bearer credential**: presenting it — by scanning the card it is written on, or by sending it over the API — authorizes switching to that profile with no PIN, on every path. Switch IDs are therefore only returned to privileged clients (local connections and admin-role paired clients) for card-writing; member clients never see them. The optional 4-8 digit **PIN** protects the remaining path: switching by `profileId` picked from the visible profile list. Leaving a profile is always free — PINs gate entry only.

**Data swapping.** On platforms that support it (currently MiSTer), the active profile also owns its save files and save states. Switching profiles makes that profile's data live; the shared profile's data is simply the default locations, so creating profiles never moves existing saves. On MiSTer this is done with bind mounts — nothing on the SD card is ever moved, copied, or rewritten, the card keeps a completely standard layout, and the per-profile data lives in plain directories under `zaparoo/profiles/<profileId>/`. The mechanism follows main's own storage rules: a USB storage root (`device.bin`) is respected, and NAS-mounted saves (cifs mount over `saves/`) are layered on — the profile pool is created inside the share (`saves/.zaparoo-profiles/<profileId>/`) so saves stay centralized, and the user's mount is never touched. A swap requested while a game is running is deferred until the game stops (the running game keeps the data it launched with, matching the playtime pinning rule). Progress and failures are reported by the [`profiles.data`](notifications.md#profilesdata) notification; the `profilesSwapData` setting turns swapping off. What never swaps: core configs and input mappings (device properties), the Recently Played lists, arcade nvram, and computer-core disk images (saves live inside the shared image). Per-profile MiSTer INI setups need no data swap — write a combo card like `**profile:<switchId>||mister.ini:2` to switch profile and INI together. Deleting a profile never deletes its data directory. Note that profile data lives under the `zaparoo/` directory: deleting a Zaparoo install directory deletes profile saves with it.

**Administration and trust model.** The first profile is created as `admin` and must have a PIN; later profiles default `member`. The first paired client is `admin`; later pairings default `member`. Sensitive local UIs call `profiles.verify`, confirm the returned profile has the `admin` role, then send the ordinary management request. This is a client-side nuisance gate for parental and kiosk controls, not cryptographic request authorization; no unlock session is retained. Admin paired clients use their client capability directly. The last admin profile/client cannot be removed or demoted. Profiles remain a household convenience boundary, comparable to TV parental controls — not OS account security. Anyone with OS access still owns the device, and while `service.encryption` is off an unpaired remote client retains legacy admin API capability. Enabling encryption makes paired-client restrictions enforceable.

### Profile object

| Key           | Type    | Required | Description                                                                                              |
| :------------ | :------ | :------- | :------------------------------------------------------------------------------------------------------- |
| profileId     | string  | Yes      | Unique identifier of the profile.                                                                        |
| name          | string  | Yes      | Display name, e.g. "Dad" or "Kid A".                                                                     |
| role          | string  | Yes      | `admin` or `member`. Admin profiles may authorize local management and must have a PIN.                     |
| switchId      | string  | No       | Word phrase written to profile switch cards, e.g. `corn-arm-truck`. A bearer credential: presenting it switches to the profile with no PIN. Returned to local callers and admin clients; omitted for remote member clients. |
| hasPin        | boolean | Yes      | True when the profile has a PIN set. The PIN itself is never returned.                                   |
| limitsEnabled | boolean | No       | Playtime limits enabled override. Omitted = inherit the global setting.                                  |
| dailyLimit    | string  | No       | Daily playtime limit override as a duration string (e.g. `2h30m`). Omitted = inherit; `0` = unlimited.   |
| sessionLimit  | string  | No       | Session playtime limit override as a duration string. Omitted = inherit; `0` = unlimited.                |
| lastUsedAt    | number  | No       | Unix timestamp of most recent successful profile activation. Omitted if never activated.                |
| createdAt     | number  | Yes      | Unix timestamp of profile creation.                                                                      |
| lastUpdatedAt | number  | Yes      | Unix timestamp of last modification.                                                                     |

### profiles

List all profiles.

#### Parameters

None.

#### Result

| Key      | Type                         | Required | Description       |
| :------- | :--------------------------- | :------- | :---------------- |
| profiles | [Profile](#profile-object)[] | Yes      | List of profiles. |

### profiles.new

Create a new profile. Remote callers require an admin client; local UIs may use `profiles.verify` as a nuisance gate. The switch ID is generated automatically; write it to a card as `**profile:<switchId>`.

#### Parameters

| Key           | Type    | Required | Description                                                      |
| :------------ | :------ | :------- | :---------------------------------------------------------------- |
| name          | string  | Yes      | Display name.                                                    |
| role          | string  | No       | `admin` or `member`; later profiles default member. First profile is always admin. |
| pin           | string  | No       | Optional 4-8 digit PIN required to switch by `profileId`; mandatory for admin profiles. |
| limitsEnabled | boolean | No       | Playtime limits enabled override.                                |
| dailyLimit    | string  | No       | Daily limit duration override.                                   |
| sessionLimit  | string  | No       | Session limit duration override.                                 |

#### Result

The created [profile object](#profile-object).

### profiles.update

Update a profile. Remote callers require an admin client; local UIs may gate this action with `profiles.verify`. Migrated profiles without an administrator may still be recovered locally. Omitted fields are unchanged. If the updated profile is currently active, its limit changes apply immediately (without resetting the running session).

#### Parameters

| Key                | Type    | Required | Description                                                            |
| :----------------- | :------ | :------- | :---------------------------------------------------------------------- |
| profileId          | string  | Yes      | Profile to update.                                                     |
| name               | string  | No       | New display name.                                                      |
| role               | string  | No       | Change between `admin` and `member`; final admin cannot be demoted.    |
| pin                | string  | No       | Set or replace the PIN; admin profiles must retain one.                |
| clearPin           | boolean | No       | Remove the PIN.                                                        |
| limitsEnabled      | boolean | No       | Playtime limits enabled override.                                      |
| dailyLimit         | string  | No       | Daily limit duration override.                                         |
| sessionLimit       | string  | No       | Session limit duration override.                                       |
| clearLimits        | boolean | No       | Reset all limit overrides back to inheriting the global config, before any limit fields in the same request are applied. |
| regenerateSwitchId | boolean | No       | Issue a new switch ID (lost-card replacement). Old cards stop working. |

#### Result

The updated [profile object](#profile-object).

### profiles.delete

Delete a profile. Remote callers require an admin client; local UIs may gate this action with `profiles.verify`. The final admin profile cannot be deleted. If it is active, the device switches to the shared profile. Past play history keeps its attribution.

#### Parameters

| Key       | Type   | Required | Description        |
| :-------- | :----- | :------- | :----------------- |
| profileId       | string | Yes      | Profile to delete.                                      |

#### Result

Null.

### profiles.active

Get the device's currently active profile.

#### Parameters

None.

#### Result

The active profile (a subset of the [profile object](#profile-object) without `switchId` and timestamps), or null when no profile is active.

### profiles.switch

Switch the device's active profile. Switching by `profileId` requires the profile's PIN when one is set. Switching by `switchId` never requires a PIN: the switch ID is a bearer credential, and presenting it is equivalent to scanning the profile's card. Calling with neither `profileId` nor `switchId` switches to the shared profile (deactivates), which never requires a PIN. Providing both is an error.

If a game is running when the profile changes, its playtime keeps counting against the profile that launched it: switching to another profile starts a fresh limit session for the new person, while deactivating leaves the launch profile's limits in force until the media stops.

#### Parameters

| Key       | Type   | Required | Description                                            |
| :-------- | :----- | :------- | :------------------------------------------------------ |
| profileId | string | No       | Profile to activate, by ID. Requires `pin` when the profile has one. |
| switchId  | string | No       | Profile to activate, by switch ID (bearer credential; no PIN needed). |
| pin       | string | No       | The profile's PIN, for `profileId` switching.          |

#### Result

The new active profile, or null when deactivated (shared profile).

### profiles.verify

Verify a profile credential **without switching**: either a profile ID plus its PIN, or a switch ID (a bearer credential — resolving it is the verification, same as scanning the card). Success returns the profile's identity and changes nothing on the device: no session, no active-profile change, no server-side grant of any kind. Clients use this to gate their own ad-hoc UI items behind a credential — e.g. a kiosk frontend requiring a parent's PIN before opening its settings screen. The security of whatever the client unlocks is entirely the client's responsibility.

PIN attempts share the same per-profile rate limiter as `profiles.switch`, so this method cannot be used to brute-force a PIN any faster than switching attempts could.

#### Parameters

| Key       | Type   | Required | Description                                              |
| :-------- | :----- | :------- | :-------------------------------------------------------- |
| profileId | string | No       | Profile to verify against. Requires `pin` when the profile has one. |
| switchId  | string | No       | Verify by switch ID (bearer credential; no PIN needed).  |
| pin       | string | No       | The profile's PIN, for `profileId` verification.         |

Exactly one of `profileId` or `switchId` is required.

#### Result

| Key       | Type    | Required | Description                              |
| :-------- | :------ | :------- | :---------------------------------------- |
| profileId | string  | Yes      | ID of the verified profile.              |
| name      | string  | Yes      | Display name of the verified profile.    |
| role      | string  | Yes      | Profile role (`admin` or `member`).      |
| hasPin    | boolean | Yes      | Whether the profile has a PIN set.       |

Verification failure (wrong PIN, unknown profile or switch ID, rate limited) returns an error, using the same errors as `profiles.switch`.

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

### launchers

List all launchers known to the running service. Suitable for populating a UI launcher picker (for example, when assigning a per-system default via [settings.update](#settingsupdate)).

#### Parameters

None.

#### Result

| Key       | Type                                  | Required | Description                  |
| :-------- | :------------------------------------ | :------- | :--------------------------- |
| launchers | [Launcher](#launcher-object)[] | Yes      | All cached launchers, sorted by `systemId` then `id`. |

##### Launcher object

| Key        | Type     | Required | Description                                                                                            |
| :--------- | :------- | :------- | :----------------------------------------------------------------------------------------------------- |
| id         | string   | Yes      | Unique launcher identifier.                                                                            |
| systemId   | string   | No       | The system this launcher targets. Omitted for generic launchers without a fixed system.                |
| systemName | string   | No       | Human-readable system name resolved from system metadata. Omitted when no metadata is available.       |
| groups     | string[] | No       | Group names this launcher belongs to. Group names are valid values for `systemDefaults.launcher`.      |

#### Example

##### Request

```json
{
  "jsonrpc": "2.0",
  "id": "5b8c3a40-7a5e-11ef-88ff-020304050607",
  "method": "launchers"
}
```

##### Response

```json
{
  "jsonrpc": "2.0",
  "id": "5b8c3a40-7a5e-11ef-88ff-020304050607",
  "result": {
    "launchers": [
      {
        "id": "retroarch",
        "systemId": "Genesis",
        "systemName": "Genesis",
        "groups": ["libretro"]
      },
      {
        "id": "snes9x",
        "systemId": "SNES",
        "systemName": "Super Nintendo",
        "groups": ["libretro"]
      }
    ]
  }
}
```

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
