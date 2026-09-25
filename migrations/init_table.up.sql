-- =========================================================
-- Auth / RBAC schema
-- Generated from ER diagram: USERS, OTP_CHALLENGES,
-- AUTH_SESSIONS, ROLES, PERMISSIONS, USER_ROLES, ROLE_PERMISSIONS
-- Target: PostgreSQL (uses gen_random_uuid() from pgcrypto)
-- =========================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- =========================================================
-- USERS
-- =========================================================
CREATE TABLE users (
    user_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name    VARCHAR(255) NOT NULL,
    email           VARCHAR(255) UNIQUE,
    phone_number    VARCHAR(32) UNIQUE,
    password_hash   TEXT NOT NULL,
    avatar_key      TEXT,
    account_status  VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- =========================================================
-- OTP_CHALLENGES  (no FK relationships in the diagram)
-- =========================================================
CREATE TABLE otp_challenges (
    challenge_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    destination     VARCHAR(255) NOT NULL,
    channel         VARCHAR(32) NOT NULL,
    purpose         VARCHAR(64) NOT NULL,
    otp_hash        TEXT NOT NULL,
    failed_attempts INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 5,
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =========================================================
-- AUTH_SESSIONS  (USERS ||--o{ AUTH_SESSIONS)
-- =========================================================
CREATE TABLE auth_sessions (
    session_id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    refresh_token_hash  TEXT NOT NULL UNIQUE,
    device_name         VARCHAR(255),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ
);

CREATE INDEX idx_auth_sessions_user_id ON auth_sessions(user_id);

-- =========================================================
-- ROLES
-- =========================================================
CREATE TABLE roles (
    role_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_name    VARCHAR(100) NOT NULL UNIQUE,
    description  TEXT
);

-- =========================================================
-- PERMISSIONS
-- =========================================================
-- CREATE TABLE permissions (
--     permission_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
--     permission_code  VARCHAR(100) NOT NULL UNIQUE,
--     description      TEXT
-- );

-- =========================================================
-- USER_ROLES  (USERS ||--o{ USER_ROLES, ROLES ||--o{ USER_ROLES)
-- Composite PK: user_id + role_id
-- =========================================================
CREATE TABLE user_roles (
    user_id      UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    role_id      UUID NOT NULL REFERENCES roles(role_id) ON DELETE CASCADE,
    assigned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX idx_user_roles_role_id ON user_roles(role_id);

-- =========================================================
-- ROLE_PERMISSIONS  (ROLES ||--o{ ROLE_PERMISSIONS, PERMISSIONS ||--o{ ROLE_PERMISSIONS)
-- Composite PK: role_id + permission_id
-- =========================================================
-- CREATE TABLE role_permissions (
--     role_id        UUID NOT NULL REFERENCES roles(role_id) ON DELETE CASCADE,
--     permission_id  UUID NOT NULL REFERENCES permissions(permission_id) ON DELETE CASCADE,
--     PRIMARY KEY (role_id, permission_id)
-- );

-- CREATE INDEX idx_role_permissions_permission_id ON role_permissions(permission_id);