// Zaparoo Core — portable TypeScript definitions for JSON-RPC 2.0 method
// signatures and responses for the media.meta and media.image API methods.
//
// These interfaces mirror the Go structs in:
//   pkg/api/models/params.go
//   pkg/api/models/responses.go

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 envelope types
// ---------------------------------------------------------------------------

export type RpcId = string | number | null;

export interface JsonRpcRequest<TParams> {
  jsonrpc: "2.0";
  id: RpcId;
  method: string;
  params?: TParams;
}

export interface JsonRpcSuccessResponse<TResult> {
  jsonrpc: "2.0";
  id: RpcId;
  result: TResult;
}

export interface JsonRpcErrorObject {
  code: number;
  message: string;
  data?: unknown;
}

export interface JsonRpcErrorResponse {
  jsonrpc: "2.0";
  id: RpcId;
  error: JsonRpcErrorObject;
}

export type JsonRpcResponse<TResult> =
  | JsonRpcSuccessResponse<TResult>
  | JsonRpcErrorResponse;

// ---------------------------------------------------------------------------
// Shared sub-types
// ---------------------------------------------------------------------------

/** A tag attached to a media record or title. count is omitted in most contexts. */
export interface TagInfo {
  tag: string;
  type: string;
  count?: number;
}

/** A single metadata property value. data is present only for binary properties. */
export interface MediaMetaPropertyItem {
  /** Base64-encoded binary blob; absent for text-only properties. */
  data?: string | null;
  /** Plain-text value or file path (when data is absent). */
  text: string;
  /** MIME type of the property value, e.g. "image/png". */
  contentType: string;
}

// ---------------------------------------------------------------------------
// media.meta
// ---------------------------------------------------------------------------

/** Parameters for the media.meta RPC method. */
export interface MediaMetaParams {
  /** Database ID of the media record to fetch. Must be > 0. */
  mediaId: number;
}

/** System sub-object returned inside a media.meta response. */
export interface MediaMetaSystemResponse {
  /** Platform system identifier, e.g. "snes". */
  id: string;
  /** Human-readable system name, e.g. "Super Nintendo Entertainment System". */
  name: string;
}

/** MediaTitle sub-object returned inside a media.meta response. */
export interface MediaMetaTitleResponse {
  /** Internal database ID of the MediaTitle record. */
  id: number;
  /** Primary slug computed from the title name. */
  slug: string;
  /** Secondary slug variant; omitted when not present. */
  secondarySlug?: string;
  /** Display name of the title. */
  name: string;
  /** Number of characters in the primary slug. */
  slugLength: number;
  /** Number of words in the primary slug. */
  slugWordCount: number;
  /** The system this title belongs to. */
  system: MediaMetaSystemResponse;
  /** Tags associated with the title. */
  tags: TagInfo[];
  /**
   * Title-level properties keyed by TypeTag (e.g. "property:image-boxart").
   * Binary values are base64-encoded in the data field.
   */
  properties: Record<string, MediaMetaPropertyItem>;
}

/** Top-level media object returned inside a media.meta response. */
export interface MediaMetaMediaResponse {
  /** Internal database ID of the media record. */
  id: number;
  /** Absolute filesystem path to the media file. */
  path: string;
  /** Parent directory of the media file. */
  parentDir: string;
  /** True when the media file no longer exists on disk. */
  isMissing: boolean;
  /** Tags associated with this media file. */
  tags: TagInfo[];
  /**
   * Media-level properties keyed by TypeTag (e.g. "property:image-boxart").
   * Media-level properties take priority over title-level properties when
   * resolving images via media.image.
   */
  properties: Record<string, MediaMetaPropertyItem>;
  /** The parent MediaTitle and its associated system. */
  title: MediaMetaTitleResponse;
}

/** Response envelope for the media.meta method. */
export interface MediaMetaResponse {
  media: MediaMetaMediaResponse;
}

/** Typed JSON-RPC request for media.meta. */
export type MediaMetaRequest = JsonRpcRequest<MediaMetaParams>;

/** Typed JSON-RPC response for media.meta. */
export type MediaMetaRpcResponse = JsonRpcResponse<MediaMetaResponse>;

// ---------------------------------------------------------------------------
// media.image
// ---------------------------------------------------------------------------

/**
 * Parameters for the media.image RPC method.
 *
 * imageTypes controls the preference order used when searching for a matching
 * image property. Each entry is a short type name:
 *   "image" | "boxart" | "screenshot" | "wheel" | "titleshot" | "map" | "marquee" | "fanart"
 *
 * "image" is an alias for "boxart". When omitted the server uses its default
 * preference order: ["image","boxart","screenshot","wheel","titleshot","map","marquee","fanart"].
 */
export interface MediaImageParams {
  /** Database ID of the media record. Must be > 0. */
  mediaId: number;
  /** Ordered list of image type names to search; optional. */
  imageTypes?: string[];
}

/** Response for the media.image method. */
export interface MediaImageResponse {
  /** MIME type of the returned image, e.g. "image/jpeg". */
  contentType: string;
  /** Base64-encoded image blob (standard encoding). */
  data: string;
  /** Full TypeTag of the matched property, e.g. "property:image-boxart". */
  typeTag: string;
}

/** Typed JSON-RPC request for media.image. */
export type MediaImageRequest = JsonRpcRequest<MediaImageParams>;

/** Typed JSON-RPC response for media.image. */
export type MediaImageRpcResponse = JsonRpcResponse<MediaImageResponse>;

// ---------------------------------------------------------------------------
// scrapers
// ---------------------------------------------------------------------------

/** One entry returned by the "scrapers" RPC method. */
export interface ScraperInfo {
  /** Stable machine-readable identifier (e.g. "gamelist.xml"). */
  id: string;
  /** Human-readable display name (e.g. "ES gamelist.xml"). */
  name: string;
}

/** Response shape for the "scrapers" JSON-RPC method. */
export interface ScrapersResponse {
  scrapers: ScraperInfo[];
}

/** Typed JSON-RPC response for scrapers. */
export type ScrapersRpcResponse = JsonRpcResponse<ScrapersResponse>;

// ---------------------------------------------------------------------------
// media.scrape
// ---------------------------------------------------------------------------

/**
 * Parameters for the "media.scrape" RPC method.
 *
 * The call returns immediately with a null result; progress is delivered via
 * "media.scraping" notifications until Done is true.
 */
export interface MediaScrapeParams {
  /** ID of the scraper to run, e.g. "gamelist.xml". Must match a value from the "scrapers" method. */
  scraperId: string;
  /**
   * Limit scraping to these system IDs.
   * Omit or pass an empty array to scrape all systems.
   */
  systems?: string[];
  /** When true, re-processes records that already carry a sentinel tag from a prior run. */
  force?: boolean;
}

/** Typed JSON-RPC request for media.scrape. */
export type MediaScrapeRequest = JsonRpcRequest<MediaScrapeParams>;

/** Typed JSON-RPC response for media.scrape (result is always null on success). */
export type MediaScrapeRpcResponse = JsonRpcResponse<null>;

// ---------------------------------------------------------------------------
// media.scrape.cancel
// ---------------------------------------------------------------------------

/** Typed JSON-RPC request for media.scrape.cancel (no params). */
export type MediaScrapeCancelRequest = JsonRpcRequest<undefined>;

/** Response for media.scrape.cancel. */
export interface MediaScrapeCancelResponse {
  message: string;
}

/** Typed JSON-RPC response for media.scrape.cancel. */
export type MediaScrapeCancelRpcResponse = JsonRpcResponse<MediaScrapeCancelResponse>;

// ---------------------------------------------------------------------------
// media.scraping  (notification)
// ---------------------------------------------------------------------------

/**
 * Payload broadcast on the "media.scraping" notification channel.
 *
 * Emitted for every ScrapeUpdate received from the running scraper and once
 * more when the run finishes or is cancelled (scraping: false, done: true).
 *
 * Mirrors Go struct: ScrapingStatusResponse (pkg/api/models/responses.go).
 */
export interface ScrapingStatusNotification {
  /** ID of the scraper that is running, e.g. "gamelist.xml". Omitted on the final done-only event. */
  scraperId?: string;
  /** System currently being scraped. Omitted between system transitions. */
  systemId?: string;
  /** Number of records processed so far in the current system. */
  processed: number;
  /** Total records to process in the current system (0 before the first system starts). */
  total: number;
  /** Number of records successfully matched and written to the DB. */
  matched: number;
  /** Number of records skipped (already scraped and force was false). */
  skipped: number;
  /** True while scraping is in progress, false on the terminal event. */
  scraping: boolean;
  /** True on the final notification for this run. */
  done: boolean;
}

/** Full JSON-RPC notification envelope for "media.scraping". */
export type MediaScrapingNotification = {
  jsonrpc: "2.0";
  method: "media.scraping";
  params: ScrapingStatusNotification;
};