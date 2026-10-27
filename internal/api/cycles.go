package api

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

var billingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// NextMonthlyReset keeps the original day, so Jan 31 -> Feb 28 -> Mar 31.
// It advances from the anchor rather than adding a month to the clamped result.
func NextMonthlyReset(anchor, after time.Time) time.Time {
	return nextMonthlyResetDay(anchor, after, 0)
}
func nextMonthlyResetDay(anchor, after time.Time, day int) time.Time {
	a, n := anchor.In(billingLocation), after.In(billingLocation)
	if day == 0 {
		day = a.Day()
	}
	months := max(1, (n.Year()-a.Year())*12+int(n.Month()-a.Month()))
	for {
		first := time.Date(a.Year(), a.Month()+time.Month(months), 1, a.Hour(), a.Minute(), a.Second(), a.Nanosecond(), billingLocation)
		last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, billingLocation).Day()
		next := time.Date(first.Year(), first.Month(), min(day, last), a.Hour(), a.Minute(), a.Second(), a.Nanosecond(), billingLocation)
		if next.After(n) {
			return next
		}
		months++
	}
}

func resetPeriods(ctx context.Context, tx pgx.Tx) error {
	var now time.Time
	if e := tx.QueryRow(ctx, "SELECT now()").Scan(&now); e != nil {
		return e
	}
	ids := []string{}
	rows, e := tx.Query(ctx, `UPDATE proxy_users SET used_bytes=0,upload_bytes=0,download_bytes=0,last_reset_at=now() WHERE tenant_id=$1 AND last_reset_at < CASE reset_strategy WHEN 'daily' THEN date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai' WHEN 'monthly' THEN date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai' ELSE '-infinity'::timestamptz END RETURNING id`, tenant)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	rows, e = tx.Query(ctx, `SELECT id,reset_anchor_at,reset_day FROM proxy_users WHERE tenant_id=$1 AND reset_strategy='rolling_monthly' AND next_reset_at<=now() FOR UPDATE`, tenant)
	if e != nil {
		return e
	}
	type cycle struct {
		id     string
		anchor time.Time
		day    int
	}
	due := []cycle{}
	for rows.Next() {
		var c cycle
		if e = rows.Scan(&c.id, &c.anchor, &c.day); e != nil {
			rows.Close()
			return e
		}
		due = append(due, c)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, c := range due {
		if _, e = tx.Exec(ctx, `UPDATE proxy_users SET used_bytes=0,upload_bytes=0,download_bytes=0,last_reset_at=now(),next_reset_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant, c.id, nextMonthlyResetDay(c.anchor, now, c.day)); e != nil {
			return e
		}
		ids = append(ids, c.id)
	}
	if len(ids) == 0 {
		return nil
	}
	if _, e = tx.Exec(ctx, `UPDATE live_services SET version=version+1,state='pending' WHERE managed_users AND tenant_id=$1 AND id IN (SELECT gs.service_id FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=$1 AND gm.user_id=ANY($2))`, tenant, ids); e != nil {
		return e
	}
	for _, id := range ids {
		if e = audit(ctx, tx, "control", "user.traffic-period-reset", id); e != nil {
			return e
		}
	}
	return nil
}

func configureCycle(ctx context.Context, tx pgx.Tx, id string, anchor *time.Time, changed bool) error {
	var strategy string
	var day int
	var current, now time.Time
	var next *time.Time
	if e := tx.QueryRow(ctx, `SELECT reset_strategy,reset_anchor_at,next_reset_at,now(),reset_day FROM proxy_users WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&strategy, &current, &next, &now, &day); e != nil {
		return e
	}
	if strategy != "rolling_monthly" {
		_, e := tx.Exec(ctx, `UPDATE proxy_users SET next_reset_at=NULL WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return e
	}
	if anchor != nil {
		if anchor.IsZero() || anchor.After(now.AddDate(1, 0, 0)) || anchor.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
			return bad("invalid reset anchor")
		}
		current = *anchor
	} else if changed || next == nil {
		current = now
	}
	if anchor == nil && !changed && next != nil {
		return nil
	}
	_, e := tx.Exec(ctx, `UPDATE proxy_users SET reset_anchor_at=$3,next_reset_at=$4 WHERE tenant_id=$1 AND id=$2`, tenant, id, current, nextMonthlyResetDay(current, now, day))
	return e
}
