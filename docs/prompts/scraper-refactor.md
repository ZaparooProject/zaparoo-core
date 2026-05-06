## Scraper Interface refactor

I don't like the ergonomics of scraping at the moment and want to explore a broad refactor before exploring other scraper types.



## Setup and definitions

Scrapers are currently loaded into the RequestEnv context from 
@pkg\api\server.go StartWithReady()
This leads to a hard-coding universal to all platforms and a pretty poor experience.

Instead the scraper configuration should be defined in the Platform interface.
@pkg\platforms\platforms.go

## Struct vs Interface
A struct with a scrapeFn() callback can achieve the results of the scraper interface a bit more clearly, leaving the logic of the scraper truly up to the implementation.

In platforms.go many structs (like Launcher) are defined with similar ergonomics.
platforms.go should expect a map of scraper structs by Scraper.ID
```
type Platform interface{
    ...
    Scrapers(*config.Instance) map[string]Scraper
}

```


```
type ScraperCustomOption struct{
	Name string
	Value string
}
type ScraperCustomOptions map[string][]ScraperCustomOption
type Scraper struct {
	ID string
	Name string
	SupportedSystemIDs []string
	CustomOpts ScraperCustomOptions
	Scrape func(*config.Instance, Platform, afero.Fs, *database.Database, ScrapeOptions, ScraperCustomOptions, <-chan ScrapeUpdate) (error)
}
```

## Scrape state and logic

Because scrapers live as lazy definitions in the platform, the RequestEnv can be simplified to not include the scrapers map.
API methods that need scraper definition data, or to execute the scrape, can do so directly from the platform intance based on mapped ID.

## Helpers
Helpers are still useful for common tasks like tag-media relationship setup, DB update flows, and path normalization. However, instead of there being a generic RunScraper handler that worries about receiving abstracted update sets, the Scraper.Scrape function should be able to handle these bahaviors as it sees fit.

##  Current state audit

There is a GamelistXMLScraper implemented with the existing interface/runner logic. @docs\prompts\scraper-reqs.md contains details on expectations but I expect the end result of the Media/Title Tags and Properties to be the same after a migration and run.