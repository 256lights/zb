update "builds"
set
  "ended_at" = :timestamp_millis,
  "internal_error" = :build_error
where "uuid" = uuid(:build_id);
