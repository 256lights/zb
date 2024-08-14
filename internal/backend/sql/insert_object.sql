insert into "objects" (
  "id",
  "nar_size",
  "nar_hash",
  "deriver",
  "ca"
) values (
  (select "id" from "paths" where "path" = :path),
  :nar_size,
  nullif(:nar_hash, ''),
  iif(:deriver is null or :deriver = '',
    null,
    (select "id" from "paths" where "path" = :deriver)),
  nullif(:ca, '')
);
