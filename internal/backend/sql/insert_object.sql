insert into "objects" (
  "id",
  "nar_size",
  "nar_hash",
  "ca"
) values (
  (select "id" from "paths" where "path" = :path),
  :nar_size,
  nullif(:nar_hash, ''),
  nullif(:ca, '')
);
