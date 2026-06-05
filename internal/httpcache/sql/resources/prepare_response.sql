update "resources"
set
  "response_received_at" = :received_at,
  "status_code" = :status_code,
  "response_body" = zeroblob(:body_size)
where "id" = :id;
