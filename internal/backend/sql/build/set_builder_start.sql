update "build_results"
set "builder_started_at" = :timestamp_millis
where "id" = :id;
