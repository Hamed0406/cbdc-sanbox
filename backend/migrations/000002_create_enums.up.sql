-- Define all ENUM types used across the schema.
-- ENUMs are preferred over VARCHAR for status fields:
-- - The database enforces valid values — no invalid status can slip through
-- - Queries on enum columns use integer comparison internally (fast)
-- - The allowed values are self-documenting in the schema

CREATE TYPE user_role AS ENUM ('user', 'merchant', 'admin');

CREATE TYPE transaction_status AS ENUM (
    'PENDING',    -- locks acquired, processing in progress
    'CONFIRMED',  -- ledger entries written, balance updated
    'SETTLED',    -- final cleared state, cannot be modified
    'FAILED',     -- rejected due to validation error or insufficient funds
    'REFUNDED'    -- reversed by a subsequent refund transaction
);

CREATE TYPE transaction_type AS ENUM (
    'ISSUANCE',   -- central bank mints new CBDC (admin only)
    'TRANSFER',   -- peer-to-peer wallet payment
    'PAYMENT',    -- wallet to merchant via payment request
    'REFUND',     -- reversal of a PAYMENT transaction
    'ADJUSTMENT'  -- administrative correction (rare)
);

CREATE TYPE ledger_entry_type AS ENUM (
    'DEBIT',   -- funds leaving a wallet
    'CREDIT'   -- funds entering a wallet
);

CREATE TYPE payment_request_status AS ENUM (
    'PENDING',    -- created, awaiting payer
    'PAID',       -- successfully paid
    'EXPIRED',    -- passed expiry_at without payment
    'CANCELLED'   -- merchant cancelled before payment
);
