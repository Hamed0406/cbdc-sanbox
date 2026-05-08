-- Enable UUID generation and cryptographic functions.
-- These are PostgreSQL built-in extensions — no external packages needed.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
