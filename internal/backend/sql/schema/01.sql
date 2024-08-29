-- Copyright 2024 Roxy Light
-- SPDX-License-Identifier: MIT

-- Store paths keyed by integers to save space.
create table "paths" (
  "id" integer primary key not null,
  "path" text unique not null
);

-- Objects that exist within the store.
create table "objects" (
  "id" integer primary key
    not null
    references "paths",
  "nar_size" integer
    not null
    check ("nar_size" > 0),
  "nar_hash" text,
  "ca" text
);

-- Store object references.
create table "references" (
  "referrer" integer not null
    references "objects" on delete cascade,
  "reference" integer not null
    references "objects" on delete restrict,
  primary key ("referrer", "reference")
) without rowid;

create index "back_references" on "references"("reference");

create table "realizations" (
  "drv_path" integer
    not null
    references "paths",
  "output_name" text
    not null
    default 'out',
  "output_path" integer
    not null
    references "paths",

  primary key ("drv_path", "output_name", "output_path")
) without rowid;

create index "realizations_by_output_path" on "realizations"("output_path");
