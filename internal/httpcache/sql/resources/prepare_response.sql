update "resources"
set
  "requested_at" = :requested_at,
  "response_received_at" = :received_at,
  "status_code" = :status_code,
  "response_body" = zeroblob(:body_size)
where "id" = :id;
