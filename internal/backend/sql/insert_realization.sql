insert into "realizations" (
  "drv_hash",
  "output_name",
  "output_path"
) values (
  (select "id" from "drv_hashes" where ("algorithm", "bits") = (:drv_hash_algorithm, :drv_hash_bits)),
  :output_name,
  (select "id" from "paths" where "path" = :output_path)
) on conflict ("drv_hash", "output_name", "output_path") do nothing;
