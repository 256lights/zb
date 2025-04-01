-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

create table "builds" (
  "id" integer primary key
    not null,
  "uuid" blob
    unique
    not null,
  "started_at" integer, -- Milliseconds since Unix epoch
  "ended_at" integer,   -- Milliseconds since Unix epoch
  "internal_error" text,

  "expand_builder" text,
  "expand_args" text
    check (
      "expand_args" is null or
      json_type("expand_args") = 'array'
    ),
  "expand_env" text
    check (
      "expand_env" is null or
      json_type("expand_env") = 'object'
    )
);

create index "builds_by_time_desc"
  on "builds" (coalesce("ended_at", "started_at") desc, "uuid");

create table "build_results" (
  "id" integer primary key
    not null,
  "build_id" integer
    not null
    references "builds" on delete cascade,
  "drv_path" integer
    not null
    references "paths",
  "status" text
    not null
    default 'active',
  "started_at" integer,         -- Milliseconds since Unix epoch
  "builder_started_at" integer, -- Milliseconds since Unix epoch
  "builder_ended_at" integer,   -- Milliseconds since Unix epoch
  "ended_at" integer,           -- Milliseconds since Unix epoch

  unique ("build_id", "drv_path")
);

create table "build_outputs" (
  "result_id" integer
    not null
    references "build_results" on delete cascade,
  "output_name" text
    not null
    default 'out',
  "output_path" integer
    references "paths",

  primary key ("result_id", "output_name")
) without rowid;

create table "build_logs" (
  "id" integer primary key
    not null,
  "result_id" integer
    not null
    references "build_results" on delete cascade,
  "seq" integer
    not null
    check ("seq" >= 1),
  "received_at" integer, -- Milliseconds since Unix epoch
  "data" blob
    not null
    check (octet_length("data") > 0),

  unique ("result_id", "seq")
);
