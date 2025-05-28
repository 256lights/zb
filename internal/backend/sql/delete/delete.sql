delete from "objects"
where "id" = (
  select "paths"."id"
  from "paths"
  where "paths"."path" = :path
);
