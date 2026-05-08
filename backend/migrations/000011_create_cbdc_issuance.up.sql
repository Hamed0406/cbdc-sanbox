-- CBDC issuance log tracks all money-creation events separately from regular transactions.
-- In real CBDC systems, this is a regulatory requirement:
-- every act of minting new currency must be independently traceable.
--
-- This table is also append-only — issuance records are never modified after creation.

CREATE TABLE cbdc_issuance (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    admin_id        UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    wallet_id       UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    transaction_id  UUID NOT NULL REFERENCES transactions(id) ON DELETE RESTRICT,
    amount_cents    BIGINT NOT NULL,
    reason          TEXT NOT NULL,    -- mandatory justification for the issuance
    ip_address      INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT cbdc_issuance_amount_positive CHECK (amount_cents > 0),
    -- DD$1,000,000 per action maximum — prevents accidental hyperinflation
    CONSTRAINT cbdc_issuance_amount_cap CHECK (amount_cents <= 100000000)
);

CREATE INDEX idx_cbdc_issuance_admin ON cbdc_issuance (admin_id, created_at DESC);
CREATE INDEX idx_cbdc_issuance_wallet ON cbdc_issuance (wallet_id, created_at DESC);

COMMENT ON TABLE cbdc_issuance IS 'Regulatory trail of all currency creation events. Append-only.';
