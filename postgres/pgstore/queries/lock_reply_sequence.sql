SELECT pg_advisory_xact_lock(hashtext($1::text || ':' || $2::text), hashtext($3::text))
