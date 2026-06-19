delete from "response_headers"
where
  "resource_id" = :id and
  (select "headers"."name"
    from "headers"
    where "headers"."id" = "response_headers"."header_id") = :name;
