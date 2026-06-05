create table if not exists "mem"."query_headers" (
  "name" text
    not null
    primary key
    collate headerkey
    check ("name" <> ''),
  "value" text
) strict;
