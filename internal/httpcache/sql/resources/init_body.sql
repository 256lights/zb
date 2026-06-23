update "resources"
set "response_body" = zeroblob(:size)
where "id" = :id;
