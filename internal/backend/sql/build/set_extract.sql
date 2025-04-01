update "builds"
set
  "expand_builder" = :builder,
  "expand_args" = :args,
  "expand_env" = :env
where "uuid" = uuid(:build_id);
