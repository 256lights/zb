insert into "response_headers" (
  "resource_id",
  "index",
  "header_id"
) values (
  :id,
  coalesce((select max("index")
    from "response_headers"
    where "resource_id" = :id) + 1, 0),
  :header_id
);
