select
  "drv_path"."path" as "drv_path",
  "build_results"."status" as "status",
  "build_results"."started_at" as "started_at",
  "build_results"."ended_at" as "ended_at",
  "build_results"."builder_started_at" as "builder_started_at",
  "build_results"."builder_ended_at" as "builder_ended_at",
  "outputs"."output_name" as "output_name",
  "output_path"."path" as "output_path"
from
  "build_results"
  join "builds" on "builds"."id" = "build_results"."build_id"
  join "paths" as "drv_path" on "drv_path"."id" = "build_results"."drv_path"
  left join "build_outputs" as "outputs" on "outputs"."result_id" = "build_results"."id"
  left join "paths" as "output_path" on "output_path"."id" = "outputs"."output_path"
where
  "builds"."uuid" = uuid(:build_id)
order by
  "build_results"."started_at" asc,
  "drv_path"."path",
  "outputs"."output_name";
