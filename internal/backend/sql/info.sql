select
  "nar_size" as "nar_size",
  "nar_hash" as "nar_hash",
  "ca" as "ca"
from
  "objects"
  join "paths" using ("id")
where "path" = :path
limit 1;
