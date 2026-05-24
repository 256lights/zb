delete from server_ttl;
insert into server_ttl (ttl_logged_at) values (:now);
