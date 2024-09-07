select
  "output_path"."path" as "output_path"
from
  "realizations"
  join "paths" as "output_path" on "realizations"."output_path" = "output_path"."id"
  -- Extra join to ensure realization exists in store.
  join "objects" on "realizations"."output_path" = "objects"."id"
where
  "drv_hash" = (select "id" from "drv_hashes" where ("algorithm", "bits") = (:drv_hash_algorithm, :drv_hash_bits)) and
  "output_name" = :output_name
order by 1;
