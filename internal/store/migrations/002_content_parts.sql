-- Add multi-modal content parts to messages.

ALTER TABLE messages ADD COLUMN content_parts TEXT;
