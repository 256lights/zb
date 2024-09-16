select distinct "path" as "path"
from
  "store_objects"
  join "source_to_store" on "source_to_store"."store_object_id" = "store_objects"."id"
where
  store_path_name("path") = :name and
  not exists (select 1
    from
      "source_to_store_files"
      join "source_files" on "source_to_store_files"."source_file_id" = "source_files"."id"
      full join temp."curr" on "source_files"."path" = temp."curr"."path"
    where
      "source_to_store_files"."mapping_id" = "source_to_store"."mapping_id" and
      ("source_files"."stamp" is null or
        temp."curr"."stamp" is null or
        "source_files"."stamp" <> temp."curr"."stamp")
  );
