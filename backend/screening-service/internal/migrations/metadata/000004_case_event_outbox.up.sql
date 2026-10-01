CREATE TABLE IF NOT EXISTS screening.case_event_outbox (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL,
 kind TEXT NOT NULL CHECK (kind IN ('reviewed','evidence-uploaded')),
 payload JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivering','delivered','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_until TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 delivered_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS case_event_outbox_dispatch_idx ON screening.case_event_outbox(available_at,created_at,id) WHERE status IN ('pending','delivering');
