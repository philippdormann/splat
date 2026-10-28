-- Chronic-regressor signal: how many times this issue has flipped back to
-- unresolved via a new occurrence (resolve -> event, lapsed mute -> event).
-- Incremented on the ingest path whenever the upsert reports reopened.
ALTER TABLE issues ADD COLUMN reopened_count INTEGER NOT NULL DEFAULT 0;
