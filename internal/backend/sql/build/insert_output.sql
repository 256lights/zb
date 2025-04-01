insert into "build_outputs" (
  "result_id",
  "output_name",
  "output_path"
) values (
  :id,
  :output_name,
  iif(
    coalesce(:output_path, '') <> '',
    (select "id" from "paths" where "path" = :output_path),
    null
  )
);
