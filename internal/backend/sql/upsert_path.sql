insert into "paths" ("path") values (:path)
  on conflict ("path") do nothing;
