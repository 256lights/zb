-- Copyright 2024 The zb Authors
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

create table "drv_hashes" (
  "id" integer primary key not null,
  "algorithm" text not null,
  "bits" blob not null,

  unique ("algorithm", "bits")
);

create table "realizations" (
  "drv_hash" integer
    not null
    references "drv_hashes",
  "output_name" text
    not null
    default 'out',
  "output_path" integer
    not null
    references "paths",

  primary key ("drv_hash", "output_name", "output_path")
) without rowid;

create index "realizations_by_output_path" on "realizations"("output_path");

create table "reference_classes" (
  "id" integer primary key not null,

  "referrer" integer not null,
  "referrer_drv_hash" integer not null,
  "referrer_output_name" text not null,

  "reference" integer not null,
  "reference_drv_hash" integer
    references "drv_hashes",
  "reference_output_name" text,

  foreign key ("referrer_drv_hash", "referrer_output_name", "referrer") references "realizations"
    on delete cascade,
  -- Foreign key constraint is only checked if all fields are non-NULL.
  foreign key ("reference_drv_hash", "reference_output_name", "reference") references "realizations"
    on delete restrict,
  check (("reference_drv_hash" is null) = ("reference_output_name" is null))
);

create index "reference_classes_by_realization" on "reference_classes" (
  "referrer_drv_hash",
  "referrer_output_name",
  "referrer"
);

create unique index "reference_classes_by_reference" on "reference_classes" (
  "referrer",
  "reference",
  "referrer_drv_hash",
  "referrer_output_name",
  "reference_drv_hash",
  "reference_output_name"
);
