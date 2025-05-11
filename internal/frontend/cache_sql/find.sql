select distinct "path" as "path"
from
  "store_objects"
  join "source_to_store" on "source_to_store"."store_object_id" = "store_objects"."id"
where
  store_path_name("path") = :name and
  not exists (
    with
      "source_files_for_mapping" as (
        select
          "path" as "path",
          "stamp" as "stamp"
        from
          "source_to_store_files"
          join "source_files" on "source_to_store_files"."source_file_id" = "source_files"."id"
        where
          "source_to_store_files"."mapping_id" = "source_to_store"."mapping_id"
      )
    select *
    from
      (
        select 1
        from
          "source_files_for_mapping"
          left join temp."curr" using ("path")
        where
          "source_files_for_mapping"."stamp" is null or
          temp."curr"."stamp" is null or
          "source_files_for_mapping"."stamp" <> temp."curr"."stamp"
        union all
        select 1
        from temp."curr"
        where "path" not in (select "path" from "source_files_for_mapping")
      )
  );
