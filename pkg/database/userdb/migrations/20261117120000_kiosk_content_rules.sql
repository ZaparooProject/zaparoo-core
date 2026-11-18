-- +goose Up
-- +goose StatementBegin

-- What a profile may launch while the device is locked. ProfileID is empty
-- for the shared profile, which is what applies when no profile is active.
-- Mode is 'all' (everything except denied) or 'allowed' (only what an allow
-- entry matches). A profile with no row uses the device's default mode.
create table ProfileContentModes (
    ProfileID text primary key,
    Mode      text not null
);

-- One allow or deny entry of a profile's content rule. Kind names what the
-- entry matches: a whole system, a deck, one media path or a tag filter.
-- SystemID and Path identify a system or a media path; Value holds a deck ID
-- or a tag filter. Kind is free text so later kinds need no schema change.
create table ProfileContentRules (
    DBID      integer primary key,
    ProfileID text not null,
    Effect    text not null,
    Kind      text not null,
    SystemID  text not null default '',
    Path      text not null default '',
    Value     text not null default ''
);

create index ProfileContentRulesProfileIdx on ProfileContentRules (ProfileID);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index ProfileContentRulesProfileIdx;
drop table ProfileContentRules;
drop table ProfileContentModes;
-- +goose StatementEnd
