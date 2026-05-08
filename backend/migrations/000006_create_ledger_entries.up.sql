-- Double-entry ledger: every transaction creates exactly 2 entries.
-- Example: Alice sends DD$25 to Bob:
--   INSERT ledger_entries (wallet_id=Alice, type=DEBIT,  amount=2500, balance_after=7500)
--   INSERT ledger_entries (wallet_id=Bob,   type=CREDIT, amount=2500, balance_after=10000)
--
-- The SUM of all entries for a wallet equals its true balance.
-- wallet.balance is a cached copy of this sum, updated atomically in the same transaction.
-- A nightly integrity job verifies these match and logs any discrepancy.
--
-- WHY store balance_after?
-- Allows reconstructing the full balance history at any point in time.
-- Critical for dispute resolution and audits.

CREATE TABLE ledger_entries (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    wallet_id       UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    transaction_id  UUID NOT NULL REFERENCES transactions(id) ON DELETE RESTRICT,
    entry_type      ledger_entry_type NOT NULL,
    amount_cents    BIGINT NOT NULL,                  -- always positive; direction set by entry_type
    balance_after   BIGINT NOT NULL,                  -- wallet balance after this entry was applied

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_entries_amount_positive CHECK (amount_cents > 0),
    -- balance_after can never be negative — this is a hard financial invariant
    CONSTRAINT ledger_entries_balance_non_negative CHECK (balance_after >= 0)
);

-- Primary query: full ledger history for a wallet in chronological order
CREATE INDEX idx_ledger_wallet_id ON ledger_entries (wallet_id, created_at DESC);
-- Lookup all entries for a specific transaction (to verify double-entry)
CREATE INDEX idx_ledger_transaction_id ON ledger_entries (transaction_id);
