-- The dispatcher must distinguish ordinary create from snapshot clone.
-- Apply before deploying the matching dispatcher; drain old dispatch workers
-- before admitting clone commands. Existing rows retain ordinary create.
ALTER TABLE sys_sandbox_create_intents
  ADD COLUMN command_kind text NOT NULL DEFAULT 'create',
  ADD CONSTRAINT sys_sandbox_create_intents_command_kind_check
    CHECK (command_kind IN ('create', 'checkpoint_clone'));
