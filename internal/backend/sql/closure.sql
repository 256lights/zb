with
  "closure"("path_id", "drv_hash_id", "output_name") as (
    select
        "paths"."id",
        iif(
          :drv_hash_algorithm is not null and
          length(:drv_hash_algorithm) > 0 and
          :drv_hash_bits is not null and
          length(:drv_hash_bits) > 0,
          (select "id" from "drv_hashes" where
            ("algorithm", "bits") = (:drv_hash_algorithm, :drv_hash_bits)),
          null),
        nullif(:output_name, '')
      from
        "paths"
        -- Ensure that object exists in store.
        join "objects" using ("id")
      where
        "path" = :path
    union
      select
        r."reference",
        rc."reference_drv_hash",
        rc."reference_output_name"
      from
        "closure"
        join "references" as r on "closure"."path_id" = r."referrer"
        left join "reference_classes" as rc on
          (r."referrer", r."reference") = (rc."referrer", rc."reference") and
          ("closure"."drv_hash_id", "closure"."output_name") is (rc."referrer_drv_hash", rc."referrer_output_name")
  )

select
  "paths"."path" as "path",
  "drv_hashes"."algorithm" as "drv_hash_algorithm",
  "drv_hashes"."bits" as "drv_hash_bits",
  "output_name" as "output_name"
from
  "closure"
  join "paths" on "closure"."path_id" = "paths"."id"
  left join "drv_hashes" on "closure"."drv_hash_id" = "drv_hashes"."id"
order by 1, 2, 3, 4;
