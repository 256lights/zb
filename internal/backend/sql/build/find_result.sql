select
  "build_results"."id" as "id",
  "build_results"."status" as "status",
  "build_results"."started_at" as "started_at",
  "build_results"."ended_at" as "ended_at",
  "build_results"."builder_started_at" as "builder_started_at",
  "build_results"."builder_ended_at" as "builder_ended_at"
from
  "build_results"
  join "paths" as "drv_path" on "drv_path"."id" = "build_results"."drv_path"
  join "builds" on "builds"."id" = "build_results"."build_id"
where
  "builds"."uuid" = uuid(:build_id) and
  "drv_path"."path" = :drv_path
limit 1;
