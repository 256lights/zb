with recursive
  "closure" ("id") as (
    select "objects"."id"
    from
      "paths_to_delete"
      join "paths" using ("path")
      join "objects" using ("id")
    union
    select r."referrer"
    from
      "closure"
      join "references" as r on "closure"."id" = r."reference"
    where
      r."referrer" <> r."reference"
  )
select
  "paths"."path" as "path"
from
  "closure"
  join "paths" using ("id")
where
  "paths"."path" not in (select "paths_to_delete"."path" from "paths_to_delete");
