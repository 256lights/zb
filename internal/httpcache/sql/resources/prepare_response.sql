update "resources"
set
  "requested_at" = :requested_at,
  "response_received_at" = :received_at,
  "status_code" = :status_code,
  "stale" = false
where "id" = :id;
