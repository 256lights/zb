insert into "store_objects"("path")
values (:path)
on conflict ("path") do nothing;
