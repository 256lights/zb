select
  "signature_public_keys"."format" as "format",
  "signature_public_keys"."public_key" as "public_key",
  "signatures"."signature" as "signature"
from
  "signatures"
  join "signature_public_keys" on "signature_public_keys"."id" = "signatures"."public_key_id"
  join "build_results" on "build_results"."drv_hash" = "signatures"."drv_hash"
  join "paths" as "drv_path" on "drv_path"."id" = "build_results"."drv_path"
  join "paths" as "output_path" on "output_path"."id" = "signatures"."output_path"
  join "builds" on "builds"."id" = "build_results"."build_id"
where
  "builds"."uuid" = uuid(:build_id) and
  "drv_path"."path" = :drv_path and
  "signatures"."output_name" = :output_name and
  "output_path"."path" = :output_path
order by
  "signature_public_keys"."format",
  "signature_public_keys"."public_key",
  "signatures"."signature";
