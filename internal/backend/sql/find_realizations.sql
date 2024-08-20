select
  "output_name" as "output_name",
  "output_path"."path" as "output_path"
from
  "realizations"
  join "paths" as "output_path" on "realizations"."output_path" = "output_path"."id"
where
  "drv_path" = (select "id" from "paths" where "path" = :drv_path)
order by 1, 2;
