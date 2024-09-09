insert into "references" ("referrer", "reference")
values (
  (select "id" from "paths" where "path" = :referrer),
  (select "id" from "paths" where "path" = :reference)
);
