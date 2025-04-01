update "build_results"
set
  "status" = :status,
  "ended_at" = :timestamp_millis
where "id" = :id;
