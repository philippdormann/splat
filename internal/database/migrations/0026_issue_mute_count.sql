-- Count-based mute ("mute the next N appearances"): muted_until_count holds
-- the remaining suppressed occurrences while Status is muted; 0 means no
-- countdown (forever or timed mute). Each ingested event decrements it and
-- the event that reaches 0 flips the issue back to unresolved (reported as
-- reopened so alert rules fire), mirroring how timed mutes lift.
ALTER TABLE issues ADD COLUMN muted_until_count INTEGER NOT NULL DEFAULT 0;
