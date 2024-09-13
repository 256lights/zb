select exists(select 1 from "objects"
  where "id" = (select "id" from "paths" where "path" = :path));
