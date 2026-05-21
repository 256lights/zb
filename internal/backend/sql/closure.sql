with
  "valid_objects"("id") as (
    select "id" from "objects"
    union
    select "output_path" from "realizations"
    union
    select "referrer" from "reference_classes"
  ),
  "normalized_references"("referrer", "referrer_drv_hash", "referrer_output_name", "reference", "reference_drv_hash", "reference_output_name") as (
    select
      "referrer",
      null,
      null,
      "reference",
      null,
      null
    from "references"
    union
    select
      "referrer",
      "referrer_drv_hash",
      "referrer_output_name",
      "reference",
      "reference_drv_hash",
      "reference_output_name"
    from "reference_classes"
  ),
  "closure"("path_id", "drv_hash_algorithm", "drv_hash_bits", "output_name") as (
    select
        "paths"."id",
        :drv_hash_algorithm,
        :drv_hash_bits,
        nullif(:output_name, '')
      from
        "paths"
        -- Ensure that object exists in store or is a known realization.
        join "valid_objects" using ("id")
      where "path" = :path
    union
      select
        r."reference",
        "drv_hashes"."algorithm",
        "drv_hashes"."bits",
        r."reference_output_name"
      from
        "normalized_references" as r
        join "closure" on "closure"."path_id" = r."referrer"
        left join "drv_hashes" on r."reference_drv_hash" = "drv_hashes"."id"
      where
        r."referrer" <> r."reference"
  )

select
  "paths"."path" as "path",
  "drv_hash_algorithm" as "drv_hash_algorithm",
  "drv_hash_bits" as "drv_hash_bits",
  "output_name" as "output_name"
from
  "closure"
  join "paths" on "closure"."path_id" = "paths"."id"
order by 1, 2, 3, 4;
