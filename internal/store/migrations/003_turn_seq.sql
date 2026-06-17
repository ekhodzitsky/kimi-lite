-- Add monotonic turn sequence number for session resume correctness.

ALTER TABLE turns ADD COLUMN seq INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_turns_session_seq ON turns(session_id, seq);
