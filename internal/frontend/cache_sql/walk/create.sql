create temp table "curr" (
  "path" text
    unique
    collate path,
  "mode" integer,
  "size" integer
    check ("size" is null or "size" >= 0),
  "stamp" text,
  "link_target" text
);
