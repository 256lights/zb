create table "store_objects" (
  "id" integer not null primary key,
  "path" text not null unique
);

create index "store_objects_by_name" on "store_objects" (store_path_name("path"));

create table "source_files" (
  "id" integer not null primary key,
  "path" text not null unique,
  "stamp" text
);

create table "source_to_store" (
  "mapping_id" integer not null primary key,
  "store_object_id" integer
    not null
    references "store_objects"
    on delete cascade
);

create index "store_mappings" on "source_to_store" ("store_object_id");

create table "source_to_store_files" (
  "mapping_id" integer
    not null
    references "source_to_store"
    on delete cascade,
  "source_file_id" integer
    not null
    references "source_files",

  primary key ("mapping_id", "source_file_id")
) without rowid;

create index "source_files_to_mappings" on "source_to_store_files" ("source_file_id");
