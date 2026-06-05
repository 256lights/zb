select
  "headers"."name" as "name",
  "headers"."value" as "value"
from
  "response_headers"
  join "headers" on "headers"."id" = "response_headers"."header_id"
where "response_headers"."resource_id" = :id
order by "response_headers"."index";
