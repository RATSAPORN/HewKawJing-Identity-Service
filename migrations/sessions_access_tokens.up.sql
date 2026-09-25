-- Existing sessions without access tokens require a fresh login.
ALTER TABLE auth_sessions ADD COLUMN access_token_hash TEXT UNIQUE;
ALTER TABLE auth_sessions ADD COLUMN access_expires_at TIMESTAMPTZ;

-- Enforce email identity even when registrations race or differ in case.
CREATE UNIQUE INDEX users_email_normalized_key ON users (lower(btrim(email)));
