insert into "source_files"("path", "stamp")
  select "path", "stamp"
  from temp."curr"
  where true -- to resolve parse ambiguity with ON CONFLICT
on conflict ("path") do update set
  "stamp" = excluded."stamp";

insert into "source_to_store_files"("mapping_id", "source_file_id")
select
  :mapping_id,
  "source_files"."id"
from
  temp."curr"
  join "source_files" on "source_files"."path" = temp."curr"."path";
