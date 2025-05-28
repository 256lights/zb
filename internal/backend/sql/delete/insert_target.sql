insert into "paths_to_delete" ("path")
values (:path)
on conflict ("path") do nothing;
