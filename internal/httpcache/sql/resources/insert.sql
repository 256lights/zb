insert into "resources" ("url", "requested_at")
values (:url, :requested_at)
returning "id" as "id";
