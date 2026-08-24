-- Bulk book generation job queue.
--
-- One row per uploaded Google Form export. A job is processed by a single in-process worker
-- goroutine, never in parallel with another job or with a live single-child print request --
-- the free instance's Chromium print already runs near its memory ceiling (see render.yaml).
-- State lives here rather than in memory so a job survives a dyno restart mid-batch: the
-- worker claims a queued job with an atomic UPDATE, so a restart just means the claim never
-- happened and the job stays queued for the next poll.
--
-- No child produced by a job is ever written to child_profile -- this table is pure job
-- bookkeeping, not part of the imported provider dataset, and carries no foreign key into it.
CREATE TABLE bulk_job (
    job_id          bigserial PRIMARY KEY,
    status          text NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed'))
                        DEFAULT 'queued',

    -- The uploaded file itself, kept until the job finishes so the worker can be a separate
    -- goroutine that doesn't share memory with the HTTP handler that received the upload.
    uploaded_csv    bytea NOT NULL,

    total_rows      integer,
    processed_rows  integer NOT NULL DEFAULT 0,

    -- Both NULL until the job reaches 'done'. No object storage: bytea on this row is the
    -- whole persistence story, consistent with the in-process/same-Postgres decisions this
    -- feature is built on. A job is naturally ephemeral -- an operator downloads the archive
    -- once -- so there is no retention policy here beyond "don't grow forever," which is a
    -- later concern, not a v1 one.
    archive_zip     bytea,
    report_csv      bytea,

    -- Set only when status = 'failed', which is reserved for the header row itself not
    -- matching the required seed -- a different failure mode from a bad individual row (those
    -- are accounted for inside report_csv instead, never here).
    error           text,

    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz
);

COMMENT ON TABLE bulk_job IS
    'One row per bulk book generation run from an uploaded Google Form export. Stateless per '
    'child -- no row here ever produces a child_profile record. See CLAUDE.md blocker #6.';
