insert into "signatures" (
  "drv_hash",
  "output_name",
  "output_path",
  "public_key_id",
  "signature"
) values (
  (select "id" from "drv_hashes" where ("algorithm", "bits") = (:drv_hash_algorithm, :drv_hash_bits)),
  :output_name,
  (select "id" from "paths" where "path" = :output_path),
  (select "id" from "signature_public_keys" where ("format", "public_key") = (:format, :public_key)),
  :signature
);
