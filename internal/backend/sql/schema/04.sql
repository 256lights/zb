create table "signature_public_keys" (
  "id" integer primary key not null,
  "format" text not null,
  "public_key" blob not null,

  unique ("format", "public_key")
);

create table "signatures" (
  "id" integer primary key not null,

  "drv_hash" integer not null,
  "output_name" text not null,
  "output_path" integer not null,

  "public_key_id" integer
    references "signature_public_keys",
  "signature" blob,

  foreign key ("drv_hash", "output_name", "output_path") references "realizations"
    on delete cascade
);

alter table "build_results" add column "drv_hash" integer references "drv_hashes";
