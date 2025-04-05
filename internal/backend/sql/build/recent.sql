select uuidhex("uuid") as "id"
from "builds"
order by coalesce("ended_at", "started_at") desc
limit :n;
