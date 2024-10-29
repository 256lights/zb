select
  "path" as "path",
  "mode" as "mode",
  "size" as "size",
  "link_target" as "link_target"
from temp."curr"
order by "path" collate path;
