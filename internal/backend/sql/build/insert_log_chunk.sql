insert into "build_logs" (
  "result_id",
  "seq",
  "received_at",
  "data"
) values (
  :build_result_id,
  coalesce(
    (select max("seq")
      from "build_logs"
      where "result_id" = :build_result_id
    ) + 1,
    1
  ),
  :received_at,
  :data
);
