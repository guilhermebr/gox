CREATE TABLE IF NOT EXISTS invoices (
    id       TEXT PRIMARY KEY,
    customer TEXT NOT NULL,
    amount   INTEGER NOT NULL CHECK (amount > 0)
);
