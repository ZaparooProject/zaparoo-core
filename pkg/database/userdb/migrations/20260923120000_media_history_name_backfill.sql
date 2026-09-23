-- +goose Up
-- +goose StatementBegin

-- Gives a name to history rows that were written without one.
--
-- An indexed title can end up empty: a directory whose files mostly begin with
-- a number has that prefix stripped, and a title that is only a number is left
-- with nothing. A history row copying that title is not merely blank in the
-- UI. An account refuses a play session with no media name, and the refusal
-- fails the whole upload batch, so one such row stops play-history sync for
-- the entire device until it is removed. The failure is silent apart from a
-- log line, so it is not something a user can be expected to notice or report.
--
-- Derive a name from the path instead: the file name without its extension.
-- That is not identical to the title a fresh launch would record, but it is
-- recognisable, and it is what makes the row uploadable again.
--
-- Rows with no path to derive from are left alone; nothing here can name them.
update MediaHistory
set MediaName = rtrim(
        case
            when basename like '%.%'
                then substr(basename, 1, length(rtrim(basename, replace(basename, '.', ''))) - 1)
            else basename
        end
    )
from (
    select
        DBID as target,
        substr(
            replace(MediaPath, '\', '/'),
            length(rtrim(
                replace(MediaPath, '\', '/'),
                replace(replace(MediaPath, '\', '/'), '/', '')
            )) + 1
        ) as basename
    from MediaHistory
    where trim(coalesce(MediaName, '')) = ''
      and trim(coalesce(MediaPath, '')) <> ''
) as derived
where MediaHistory.DBID = derived.target
  and trim(derived.basename) <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The previous value was an empty string; nothing is restored by undoing this.
-- +goose StatementEnd
