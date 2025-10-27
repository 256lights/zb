select
  "output_path"."path" as "output_path",
  "objects"."id" is not null as "present_in_store"
from
  "realizations"
  join "paths" as "output_path" on "realizations"."output_path" = "output_path"."id"
  left join "objects" on "realizations"."output_path" = "objects"."id"
where
  "drv_hash" = (select "id" from "drv_hashes" where ("algorithm", "bits") = (:drv_hash_algorithm, :drv_hash_bits)) and
  "output_name" = :output_name and
  (:trust_all or exists(
    select 1
    from
      "signatures"
      join "signature_public_keys" on "signature_public_keys"."id" = "signatures"."public_key_id"
    where
      ("signatures"."drv_hash", "signatures"."output_name", "signatures"."output_path") = ("realizations"."drv_hash", "realizations"."output_name", "realizations"."output_path") and
      exists(select 1 from "trusted_public_keys" where
        ("trusted_public_keys"."format", "trusted_public_keys"."public_key") =
          ("signature_public_keys"."format", "signature_public_keys"."public_key")
      )
  ))
order by 1;
