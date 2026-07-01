create table "headers" (
  "id" integer
    not null
    primary key,
  "name" text
    not null
    collate headerkey
    check ("name" not in ('', 'Authorization')),
  "value" text
    not null
    default '',

  unique("value", "name")
);

create table "resources" (
  "id" integer
    not null
    primary key,
  "url" text
    not null
    check ("url" <> ''),
  "stale" integer
    not null
    default false
    check ("stale" in (false, true)),
  "requested_at" integer -- Milliseconds since the Unix epoch
    not null,

  "response_received_at" integer, -- Milliseconds since the Unix epoch
  "status_code" integer
    not null
    default 200,
  "response_body" blob
);

create index "resources_by_url" on "resources"("url", "requested_at" desc);

create table "request_headers" (
  "resource_id" integer
    not null
    references "resources"
    on delete cascade,
  "header_id" integer
    not null
    references "headers",

  primary key ("resource_id", "header_id")
);

create table "response_headers" (
  "resource_id" integer
    not null
    references "resources"
    on delete cascade,
  "index" integer
    not null
    check ("index" >= 0),
  "header_id" integer
    not null
    references "headers",

  primary key ("resource_id", "index")
);
