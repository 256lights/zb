insert into "builds" (
  "uuid",
  "started_at"
) values (
  uuid(:build_id),
  :timestamp_millis
);
