-- The subset of auth-all's tables that app queries reference. auth-all owns the real DDL.
CREATE TABLE auth_users (
    id uuid PRIMARY KEY,
    email text NOT NULL
);
