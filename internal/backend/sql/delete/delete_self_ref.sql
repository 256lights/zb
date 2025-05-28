delete from "references"
where
  "referrer" = (
    select "paths"."id"
    from "paths"
    where "paths"."path" = :path
  ) and
  "referrer" = "reference";
