delete from "headers"
where "id" not in (
  select "header_id"
    from "request_headers"
  union all
  select "header_id"
    from "response_headers"
);
