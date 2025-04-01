with
  "chunks" as (
    select
      coalesce(
        sum(octet_length("data"))
          over (order by "seq" asc
            range between unbounded preceding and 1 preceding),
        0
      ) as "start",
      "received_at" as "received_at",
      "data" as "data"
    from "build_logs"
    where "result_id" = :build_result_id
  )
select
  max("start", :start) as "start",
  "received_at" as "received_at",
  substr(
    "data",
    max(1, :start - "start" + 1),
    coalesce(:end - "start", octet_length("data"))
  ) as "data"
from "chunks"
where
  "start" + octet_length("data") >= :start and
  (:end is null or "start" < :end)
order by "start" asc;
