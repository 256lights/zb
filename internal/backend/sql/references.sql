select
  "reference"."path" as "path"
from
  "references"
  join "paths" as "referrer" on ("references"."referrer" = "referrer"."id")
  join "paths" as "reference" on ("references"."reference" = "reference"."id")
where "referrer"."path" = :path
order by 1;
