delete from "builds"
where
  coalesce("ended_at", "started_at") < :cutoff_millis and
  -- active_builds is a temporary table that the caller will provide.
  "uuid" not in (select "uuid" from "active_builds")
returning uuidhex("uuid") as "id";
