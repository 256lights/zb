insert into "build_results" (
  "build_id",
  "drv_path",
  "started_at"
) values (
  (select "id" from "builds" where "uuid" = uuid(:build_id)),
  (select "id" from "paths" where "path" = :drv_path),
  :timestamp_millis
) returning "id";
