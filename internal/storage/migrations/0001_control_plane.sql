-- DevCadience control-plane baseline schema (M1).
--
-- Two kinds of table live here and they must not be confused:
--
--   * The event journal is the durable source of truth. It is append-only,
--     enforced by triggers rather than by convention.
--   * Everything named projection_* is derived. It exists so that common
--     queries do not replay history, and it can be dropped and rebuilt from
--     the journal at any time (DCI-053).
--
-- The `records` table sits between the two: it stores the full durable
-- protocol documents (Work Packages, validation and review results, decision
-- records) that events reference by id and digest. Those documents are
-- immutable evidence, not projections, so they are also protected from
-- update and delete.

CREATE TABLE schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    checksum   TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

-- ---------------------------------------------------------------------------
-- Event journal
-- ---------------------------------------------------------------------------

CREATE TABLE events (
    -- seq is the total order of history and the value ProjectState reports as
    -- its high-watermark. AUTOINCREMENT guarantees a sequence number is never
    -- reused, so a watermark always means the same point in history.
    seq            INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id       TEXT    NOT NULL UNIQUE,
    project_id     TEXT    NOT NULL,
    event_type     TEXT    NOT NULL,
    schema_version TEXT    NOT NULL,
    occurred_at    TEXT    NOT NULL,
    actor_kind     TEXT    NOT NULL,
    actor_id       TEXT    NOT NULL,
    -- The three hot correlation identifiers are promoted to columns for
    -- indexed lookup; the complete typed Correlation is kept as canonical
    -- JSON so no identifier is lost.
    task_id          TEXT,
    attempt_id       TEXT,
    work_package_id  TEXT,
    correlation    TEXT    NOT NULL,
    payload        TEXT    NOT NULL,
    payload_digest TEXT    NOT NULL
);

-- Reading a project's history in order is the journal's primary access
-- pattern: every projection rebuild and every state reduction performs it.
CREATE INDEX events_project_seq ON events (project_id, seq);
-- "what happened to this task" powers `task show` and escalation review.
CREATE INDEX events_task_seq ON events (project_id, task_id, seq);
-- Filtering by type supports `events list --type` and, later, metrics.
CREATE INDEX events_project_type_seq ON events (project_id, event_type, seq);

-- Events are facts that happened. A correction is a later event, never an
-- edit (docs/PROJECT_STATE.md §6). Enforcing that in the database means a
-- future bug, migration or ad-hoc sqlite3 session cannot quietly rewrite
-- history.
CREATE TRIGGER events_immutable_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only: record a correcting event instead');
END;

CREATE TRIGGER events_immutable_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only: history cannot be deleted');
END;

-- ---------------------------------------------------------------------------
-- Durable protocol records
-- ---------------------------------------------------------------------------

CREATE TABLE records (
    record_kind    TEXT    NOT NULL,
    record_id      TEXT    NOT NULL,
    -- record_version is the document's own version (Work Package revisions);
    -- it is 1 for records that have no revision concept.
    record_version INTEGER NOT NULL,
    project_id     TEXT    NOT NULL,
    schema_version TEXT    NOT NULL,
    document       TEXT    NOT NULL,
    digest         TEXT    NOT NULL,
    created_at     TEXT    NOT NULL,
    PRIMARY KEY (record_kind, record_id, record_version)
);

CREATE INDEX records_project_kind ON records (project_id, record_kind, record_id);

CREATE TRIGGER records_immutable_update
BEFORE UPDATE ON records
BEGIN
    SELECT RAISE(ABORT, 'durable records are immutable: store a new version instead');
END;

CREATE TRIGGER records_immutable_delete
BEFORE DELETE ON records
BEGIN
    SELECT RAISE(ABORT, 'durable records are immutable: historical evidence cannot be deleted');
END;

-- ---------------------------------------------------------------------------
-- Projections (derived, rebuildable)
-- ---------------------------------------------------------------------------

CREATE TABLE projection_projects (
    project_id           TEXT    PRIMARY KEY,
    name                 TEXT    NOT NULL,
    -- high_watermark is the sequence of the last event folded into this row.
    -- state_revision is derived from it, so the two can never disagree.
    high_watermark       INTEGER NOT NULL,
    state_revision       TEXT    NOT NULL,
    generated_at         TEXT    NOT NULL,
    project_state        TEXT    NOT NULL,
    project_state_digest TEXT    NOT NULL
);

CREATE TABLE projection_tasks (
    task_id              TEXT    PRIMARY KEY,
    project_id           TEXT    NOT NULL REFERENCES projection_projects (project_id) ON DELETE CASCADE,
    alias                TEXT    NOT NULL,
    title                TEXT    NOT NULL,
    milestone_id         TEXT    NOT NULL,
    change_class         TEXT    NOT NULL,
    state                TEXT    NOT NULL,
    work_package_id      TEXT,
    work_package_version INTEGER NOT NULL DEFAULT 0,
    current_attempt_id   TEXT,
    accepted_commit      TEXT,
    -- blocked holds the canonical JSON of the typed BlockedReason, or NULL.
    -- It is NULL exactly when state is not 'blocked'.
    blocked              TEXT,
    created_seq          INTEGER NOT NULL,
    updated_seq          INTEGER NOT NULL,
    -- A human-readable alias must identify exactly one task, otherwise a
    -- principal referring to "DC-012" would be ambiguous.
    UNIQUE (project_id, alias)
);

-- ProjectState buckets tasks by state on every render.
CREATE INDEX projection_tasks_state ON projection_tasks (project_id, state);

CREATE TABLE projection_attempts (
    attempt_id  TEXT    PRIMARY KEY,
    project_id  TEXT    NOT NULL,
    task_id     TEXT    NOT NULL REFERENCES projection_tasks (task_id) ON DELETE CASCADE,
    ordinal     INTEGER NOT NULL,
    status      TEXT    NOT NULL,
    -- Attempts are always read whole, for one task, so the projection keeps
    -- the typed Attempt as one canonical JSON document and promotes only the
    -- columns that are filtered or ordered on. This is a derived cache, not a
    -- durable record: the journal remains authoritative.
    document    TEXT    NOT NULL,
    created_seq INTEGER NOT NULL,
    updated_seq INTEGER NOT NULL,
    UNIQUE (task_id, ordinal)
);

CREATE INDEX projection_attempts_task ON projection_attempts (task_id, ordinal);
