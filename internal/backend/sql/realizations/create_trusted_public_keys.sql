create temp table "trusted_public_keys" (
  "format" text not null,
  "public_key" blob not null,

  unique ("format", "public_key")
);
