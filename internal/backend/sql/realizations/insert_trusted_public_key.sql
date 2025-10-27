insert into temp."trusted_public_keys" ("format", "public_key")
values (:format, :public_key)
on conflict ("format", "public_key") do nothing;
