insert into "build_results" (
  "build_id",
  "drv_path",
  "drv_hash",
  "started_at"
) values (
  (select "id" from "builds" where "uuid" = uuid(:build_id)),
  (select "id" from "paths" where "path" = :drv_path),
  (select "id" from "drv_hashes"
    where "algorithm" = :drv_hash_algorithm
    and "bits" = :drv_hash_bits),
  :timestamp_millis
) returning "id";
