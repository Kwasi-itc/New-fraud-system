package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type CaseEventRepository struct{ db queryable }

func NewCaseEventRepository(db queryable) CaseEventRepository { return CaseEventRepository{db: db} }
func (r CaseEventRepository) Enqueue(ctx context.Context, id, tenant, kind string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO screening.case_event_outbox(id,tenant_id,kind,payload) VALUES($1,$2,$3,$4)`, id, tenant, kind, body)
	return err
}

func (r CaseEventRepository) Claim(ctx context.Context) (*ports.CaseEvent, error) {
	var item ports.CaseEvent
	err := r.db.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM screening.case_event_outbox
 WHERE (status='pending' AND available_at<=now()) OR (status='delivering' AND lease_until<=now())
 ORDER BY available_at,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE screening.case_event_outbox o SET status='delivering',attempts=attempts+1,lease_until=now()+interval '60 seconds'
 FROM candidate c WHERE o.id=c.id RETURNING o.id,o.kind,o.payload,o.attempts`).Scan(&item.ID, &item.Kind, &item.Payload, &item.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}
func (r CaseEventRepository) Finish(ctx context.Context, item ports.CaseEvent, deliveryErr error) error {
	var tag pgconn.CommandTag
	var err error
	if deliveryErr == nil {
		tag, err = r.db.Exec(ctx, `UPDATE screening.case_event_outbox SET status='delivered',delivered_at=now(),lease_until=NULL,last_error='' WHERE id=$1 AND attempts=$2 AND status='delivering'`, item.ID, item.Attempts)
	} else {
		message := []rune(deliveryErr.Error())
		if len(message) > 1000 {
			message = message[:1000]
		}
		tag, err = r.db.Exec(ctx, `UPDATE screening.case_event_outbox SET status=CASE WHEN attempts>=20 THEN 'failed' ELSE 'pending' END,
 available_at=now()+make_interval(secs=>LEAST(300,power(2,LEAST(attempts,8)))::double precision),lease_until=NULL,last_error=$3
 WHERE id=$1 AND attempts=$2 AND status='delivering'`, item.ID, item.Attempts, string(message))
	}
	if err == nil && tag.RowsAffected() != 1 {
		return fmt.Errorf("case event lease was superseded")
	}
	return err
}
