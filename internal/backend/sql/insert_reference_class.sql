insert into "reference_classes" (
  "referrer",
  "referrer_drv_hash",
  "referrer_output_name",
  "reference",
  "reference_drv_hash",
  "reference_output_name"
) values (
  (select "id" from "paths" where "path" = :referrer_path),
  (select "id" from "drv_hashes" where ("algorithm", "bits") = (:referrer_drv_hash_algorithm, :referrer_drv_hash_bits)),
  :referrer_output_name,
  (select "id" from "paths" where "path" = :reference_path),
  iif(
    :reference_drv_hash_algorithm is null or
    :reference_drv_hash_algorithm = '' or
    :reference_drv_hash_bits is null or
    length(:reference_drv_hash_bits) = 0,
    null,
    (select "id" from "drv_hashes" where ("algorithm", "bits") = (:reference_drv_hash_algorithm, :reference_drv_hash_bits))
  ),
  nullif(:reference_output_name, '')
) on conflict (
  "referrer",
  "reference",
  "referrer_drv_hash",
  "referrer_output_name",
  "reference_drv_hash",
  "reference_output_name"
) do nothing;
