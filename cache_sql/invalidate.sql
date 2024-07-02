-- Clear any mappings that have changed files.
delete from "source_to_store"
where "mapping_id" in (
  select "mapping_id"
  from
    "source_to_store_files"
    join "source_files" on "source_to_store_files"."source_file_id" = "source_files"."id"
    join temp."curr" on "source_files"."path" = temp."curr"."path"
  where
    "source_files"."stamp" <> temp."curr"."stamp"
);

-- Remove any store objects that no longer have references.
delete from "store_objects"
where not exists(select 1 from "source_to_store"
  where "source_to_store"."store_object_id" = "store_objects"."id");
