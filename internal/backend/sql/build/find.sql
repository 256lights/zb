select
  case
    when "ended_at" is null then 'active'
    when "internal_error" is not null or exists(
      select 1 from "build_results"
      where
        "build_results"."build_id" = "builds"."id" and
        "build_results"."status" = 'error'
    ) then 'error'
    when exists(
      select 1 from "build_results"
      where
        "build_results"."build_id" = "builds"."id" and
        "build_results"."status" = 'fail'
    ) then 'fail'
    else 'success'
  end as "status",
  "started_at" as "started_at",
  "ended_at" as "ended_at",
  "expand_builder" is not null or
    "expand_args" is not null or
    "expand_env" is not null as "has_expand",
  "expand_builder" as "expand_builder",
  "expand_args" as "expand_args",
  "expand_env" as "expand_env"
from "builds"
where "uuid" = uuid(:build_id)
limit 1;
