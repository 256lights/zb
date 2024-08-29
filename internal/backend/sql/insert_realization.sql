insert into "realizations" (
  "drv_path",
  "output_name",
  "output_path"
) values (
  (select "id" from "paths" where "path" = :drv_path),
  :output_name,
  (select "id" from "paths" where "path" = :output_path)
) on conflict ("drv_path", "output_name", "output_path") do nothing;
