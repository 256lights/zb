update "build_results"
set "builder_ended_at" = :timestamp_millis
where "id" = :id;
