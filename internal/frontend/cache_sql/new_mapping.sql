insert into "source_to_store"("store_object_id")
values ((select "id" from "store_objects" where "path" = :path))
returning "mapping_id" as "mapping_id";
