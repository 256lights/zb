select
  "resources"."id" as "id",
  "resources"."requested_at" as "requested_at",
  "resources"."response_received_at" as "response_received_at",
  "resources"."status_code" as "status_code",
  octet_length("resources"."response_body") as "response_body_size"
from
  "resources"
where
  "url" = :url and
  not exists (select 1
    from
      "request_headers"
      join "headers" as h on "request_headers"."header_id" = h."id"
      left join "mem"."query_headers" as qh on h."name" = qh."name"
    where
      "request_headers"."resource_id" = "resources"."id" and
      h."value" is not qh."value")
order by
  coalesce(
    (
      -- Get Date header.
      select httpdate("headers"."value")
      from
        "response_headers"
        join "headers" on "headers"."id" = "response_headers"."header_id"
      where
        "response_headers"."resource_id" = "resources"."id" and
        "headers"."name" = 'Date'
      order by "response_headers"."index"
    ),
    "resources"."response_received_at"
  ) desc nulls first,
  "resources"."requested_at" desc,
  "resources"."id";
