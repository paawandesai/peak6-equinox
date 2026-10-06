-- Runs once when the local Postgres volume is first created (docker-compose.yml).
-- Creates the app and test databases, owned by the equinox user. Idempotent.
SELECT 'CREATE DATABASE equinox OWNER equinox'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'equinox')\gexec

SELECT 'CREATE DATABASE equinox_test OWNER equinox'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'equinox_test')\gexec
