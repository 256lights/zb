insert into temp."curr" (
  "path",
  "mode",
  "size",
  "stamp",
  "link_target"
) values (
  :path,
  :mode,
  iif(:size >= 0, :size, null),
  :stamp,
  iif(:stamp glob 'link:*', substr(:stamp, 6), null)
);
