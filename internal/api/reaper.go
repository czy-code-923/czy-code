package api

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func reap(ctx context.Context, tx pgx.Tx) error {
	rows, e := tx.Query(ctx, `WITH expired AS (SELECT tenant_id,id FROM tasks WHERE tenant_id=$1 AND state='running' AND lease_until<=now() FOR UPDATE SKIP LOCKED LIMIT 200)
 UPDATE tasks t SET state=CASE WHEN attempts>=3 THEN 'failed' ELSE 'queued' END,
 result=CASE WHEN attempts>=3 THEN 'lease expired after 3 attempts' ELSE 'lease expired; queued for retry' END,
 lease_token=NULL,lease_until=NULL,updated_at=now() FROM expired x WHERE (t.tenant_id,t.id)=(x.tenant_id,x.id) RETURNING t.id,t.state`, tenant)
	if e != nil {
		return e
	}
	type event struct{ id, state string }
	events := []event{}
	for rows.Next() {
		var v event
		if e = rows.Scan(&v.id, &v.state); e != nil {
			rows.Close()
			return e
		}
		events = append(events, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range events {
		if e = audit(ctx, tx, "system", "lease."+v.state, v.id); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) Reap(ctx context.Context) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(717071)`); e != nil {
		return e
	}
	if e = resetPeriods(ctx, tx); e != nil {
		return e
	}
	if e = reap(ctx, tx); e != nil {
		return e
	}
	var clock string
	e = tx.QueryRow(ctx, `INSERT INTO maintenance_clock VALUES($1,now()) ON CONFLICT(tenant_id) DO UPDATE SET last_run_at=excluded.last_run_at WHERE maintenance_clock.last_run_at<now()-interval '1 minute' RETURNING tenant_id`, tenant).Scan(&clock)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if e == nil {
		for _, q := range []string{`DELETE FROM host_retirement_receipts WHERE tenant_id=$1 AND completed_at<now()-interval '30 days'`, `DELETE FROM access_observations WHERE tenant_id=$1 AND observed_at<now()-interval '7 days'`, `DELETE FROM traffic_daily WHERE tenant_id=$1 AND day<(now() AT TIME ZONE 'Asia/Shanghai')::date-90`, `DELETE FROM deployment_batches WHERE tenant_id=$1 AND created_at<now()-interval '30 days'`} {
			if _, e = tx.Exec(ctx, q, tenant); e != nil {
				return e
			}
		}
	}
	return tx.Commit(ctx)
}
