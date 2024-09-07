insert into "drv_hashes" (
  "algorithm",
  "bits"
) values (
  :algorithm,
  :bits
) on conflict ("algorithm", "bits") do nothing;
