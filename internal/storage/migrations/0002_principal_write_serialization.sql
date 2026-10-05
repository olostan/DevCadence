-- Principal write-intent serialization row (WP-M5-1).
--
-- A deferred BEGIN followed by a read does not serialise two connections: both
-- can read the same prefix before either writes. The guarded batch writer
-- therefore executes
--
--     UPDATE principal_write_serialization SET marker = marker WHERE id = 1
--
-- before any projection or record read, which takes the SQLite write lock
-- without changing project truth. The table is not domain state: it holds one
-- immutable-lifetime row (1, 0), is never read by queries, and is never
-- deleted or recreated as recovery. A missing or corrupt row is an integrity
-- failure, not a reason to re-seed it.
CREATE TABLE principal_write_serialization (
    id     INTEGER PRIMARY KEY CHECK (id = 1),
    marker INTEGER NOT NULL CHECK (marker = 0)
);

INSERT INTO principal_write_serialization (id, marker) VALUES (1, 0);

CREATE TRIGGER principal_write_serialization_no_delete
BEFORE DELETE ON principal_write_serialization
BEGIN
    SELECT RAISE(ABORT, 'principal_write_serialization row is permanent');
END;

CREATE TRIGGER principal_write_serialization_no_id_change
BEFORE UPDATE OF id ON principal_write_serialization
BEGIN
    SELECT RAISE(ABORT, 'principal_write_serialization row is permanent');
END;
