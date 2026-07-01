select
  "headers"."name" as "name",
  "headers"."value" as "value"
from
  "request_headers"
  join "headers" on "headers"."id" = "request_headers"."header_id"
where "request_headers"."resource_id" = :id
order by 1, "headers"."id";
