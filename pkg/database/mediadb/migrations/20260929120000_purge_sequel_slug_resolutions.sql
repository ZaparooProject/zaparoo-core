-- +goose Up
-- #1561: the fuzzy, progressive-trim and main-title-prefix strategies used to
-- accept a candidate whose embedded sequel number differed from the query's,
-- or that had no number at all where the query had one. "Street Fighter II"
-- slugifies to within one character of "Street Fighter" and Jaro-Winkler
-- scored that a near-perfect typo match, so it cached a resolution to the
-- wrong game. A cache hit returns without re-running the corrected strategy
-- at all.
--
-- Two further, related changes to the raw Jaro-Winkler fuzzy strategy also
-- invalidate its cached resolutions:
--   1. Matching now additionally requires every one of the query's word
--      tokens to have a close match among the candidate's tokens, catching
--      cases like "Street Fighter II Turbo" scoring 0.927 against "Street
--      Fighter Zero 2" by whole-string character overlap alone, with
--      "turbo"/"zero" simply unrelated words.
--   2. Every result from this strategy is now discounted, since a
--      character-shape typo correction has no structural guarantee it
--      reached the right title.
--
-- Clearing the entries those three strategies wrote retires the stale ones.
-- Exact-match resolutions are untouched: strategies 1/2's own classification
-- did not change, only what the fuzzy/trim/prefix strategies accept and how
-- ties and confidence are scored. Nothing is lost: the table only memoises
-- resolutions the pipeline can redo, and the next launch of each title
-- repopulates it.
DELETE FROM SlugResolutionCache
WHERE Strategy IN ('strategy_jarowinkler_damerau', 'strategy_progressive_trim', 'strategy_main_title_only');

-- +goose Down
-- The cache rebuilds itself on use, so there is nothing to restore.
SELECT 1;
