package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"
	"time"
)

// One outbound long poll, no webhook/listening port, no worker when disabled.
// A session advisory lock prevents multiple control processes polling one bot.
func (s *Server) RunNotifications(ctx context.Context) {
	for ctx.Err() == nil {
		delay := 15 * time.Second
		if len(s.MasterKey) == 32 {
			c, _, _, e := s.loadNotifications(ctx)
			if e == nil && c.Enabled {
				// The session lock must not hold a business-pool slot during a
				// 20-second long poll (including installations with pool_max_conns=1).
				conn, e := pgx.ConnectConfig(ctx, s.DB.Config().ConnConfig.Copy())
				if e == nil {
					var locked bool
					e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(717014)`).Scan(&locked)
					if e == nil && locked {
						e = s.notificationStep(ctx)
						unlock, cancel := context.WithTimeout(context.Background(), time.Second)
						_, _ = conn.Exec(unlock, `SELECT pg_advisory_unlock(717014)`)
						cancel()
						if e == nil && c.CommandPolling {
							delay = time.Second
						}
					}
					closing, cancel := context.WithTimeout(context.Background(), time.Second)
					_ = conn.Close(closing)
					cancel()
				}
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Server) notificationCurrent(ctx context.Context, c notificationConfig) bool {
	var version int
	var raw []byte
	if s.DB.QueryRow(ctx, `SELECT version,settings FROM notification_settings WHERE tenant_id=$1`, tenant).Scan(&version, &raw) != nil || version != c.Version {
		return false
	}
	var latest notificationConfig
	return json.Unmarshal(raw, &latest) == nil && latest.Enabled
}
func (s *Server) logNotification(ctx context.Context, event string, e error) {
	state, detail := "sent", ""
	if e != nil {
		state = "failed"
		detail = e.Error()
	}
	_, _ = s.DB.Exec(ctx, `INSERT INTO notification_log(tenant_id,event,state,detail) VALUES($1,$2,$3,$4)`, tenant, event, state, detail)
	_, _ = s.DB.Exec(ctx, `UPDATE notification_settings SET last_error=$2,last_send_at=CASE WHEN $2='' THEN now() ELSE last_send_at END WHERE tenant_id=$1`, tenant, detail)
}
func (s *Server) notificationStep(ctx context.Context) error {
	c, token, offset, e := s.loadNotifications(ctx)
	if e != nil || !c.Enabled {
		return e
	}
	if c.CommandPolling {
		var updates []telegramUpdate
		e = s.telegram(ctx, token, "getUpdates", map[string]any{"offset": offset, "limit": 10, "timeout": 20, "allowed_updates": []string{"message"}}, &updates)
		if e != nil {
			s.logNotification(ctx, "poll", e)
			return e
		}
		if !s.notificationCurrent(ctx, c) {
			return nil
		}
		sent := 0
		for _, update := range updates {
			if update.ID < offset {
				continue
			}
			// Commit consumption before a reply: crash/restart cannot replay commands.
			offset = update.ID + 1
			if _, e = s.DB.Exec(ctx, `UPDATE notification_settings SET offset_id=GREATEST(offset_id,$2),last_poll_at=now() WHERE tenant_id=$1 AND version=$3`, tenant, offset, c.Version); e != nil {
				return e
			}
			command := allowedTelegramCommand(c, update, time.Now())
			if command == "" || sent >= 3 {
				continue
			}
			text, e := s.notificationSummary(ctx, command)
			if e != nil {
				return e
			}
			if !s.notificationCurrent(ctx, c) {
				return nil
			}
			e = s.sendTelegram(ctx, c, token, text)
			s.logNotification(ctx, "command."+command, e)
			if e != nil {
				return e
			}
			sent++
		}
		_, _ = s.DB.Exec(ctx, `UPDATE notification_settings SET last_poll_at=now(),last_error='' WHERE tenant_id=$1 AND version=$2`, tenant, c.Version)
	}
	if !s.notificationCurrent(ctx, c) {
		return nil
	}
	var reportDue, alertDue bool
	if e = s.DB.QueryRow(ctx, `SELECT next_report_at<=now(),next_alert_at<=now() FROM notification_settings WHERE tenant_id=$1`, tenant).Scan(&reportDue, &alertDue); e != nil {
		return e
	}
	if reportDue && c.ReportMinutes > 0 {
		sections := c.ReportSections
		if len(sections) == 0 {
			sections = []string{"status"}
		}
		parts := []string{"定时报告 · " + time.Now().UTC().Format(time.RFC3339)}
		for _, section := range sections {
			text, e := s.notificationSummary(ctx, section)
			if e != nil {
				return e
			}
			parts = append(parts, text)
		}
		e = s.sendTelegram(ctx, c, token, strings.Join(parts, "\n\n"))
		s.logNotification(ctx, "scheduled.report", e)
		if e != nil {
			return e
		}
		_, _ = s.DB.Exec(ctx, `UPDATE notification_settings SET next_report_at=now()+($2::int*interval '1 minute') WHERE tenant_id=$1 AND version=$3`, tenant, c.ReportMinutes, c.Version)
	}
	if alertDue {
		if e = s.notificationAlerts(ctx, c, token); e != nil {
			return e
		}
		_, _ = s.DB.Exec(ctx, `UPDATE notification_settings SET next_alert_at=now()+interval '1 minute' WHERE tenant_id=$1 AND version=$2`, tenant, c.Version)
	}
	_, _ = s.DB.Exec(ctx, `DELETE FROM notification_log WHERE tenant_id=$1 AND (created_at<now()-interval '7 days' OR id NOT IN (SELECT id FROM notification_log WHERE tenant_id=$1 ORDER BY id DESC LIMIT 500))`, tenant)
	_, _ = s.DB.Exec(ctx, `DELETE FROM notification_state WHERE tenant_id=$1 AND updated_at<now()-interval '30 days'`, tenant)
	return nil
}
func (s *Server) notificationAlerts(ctx context.Context, c notificationConfig, token string) error {
	type item struct{ Key, Name, State, Event string }
	items := []item{}
	if slices.Contains(c.Events, "host_offline") || slices.Contains(c.Events, "host_online") {
		rows, e := s.DB.Query(ctx, `SELECT id,name,CASE WHEN last_seen>now()-interval '90 seconds' THEN 'online' ELSE 'offline' END FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL AND last_seen IS NOT NULL LIMIT 200`, tenant)
		if e != nil {
			return e
		}
		for rows.Next() {
			var i item
			if e = rows.Scan(&i.Key, &i.Name, &i.State); e != nil {
				rows.Close()
				return e
			}
			i.Key = "host/" + i.Key
			i.Event = "host_" + i.State
			items = append(items, i)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	if slices.Contains(c.Events, "quota") {
		rows, e := s.DB.Query(ctx, `SELECT id,name,CASE WHEN quota_bytes>0 AND used_bytes::numeric*100>=quota_bytes::numeric*$2 THEN 'alert' ELSE 'ok' END FROM proxy_users WHERE tenant_id=$1 AND enabled LIMIT 500`, tenant, c.QuotaPercent)
		if e != nil {
			return e
		}
		for rows.Next() {
			var i item
			if e = rows.Scan(&i.Key, &i.Name, &i.State); e != nil {
				rows.Close()
				return e
			}
			i.Key = "quota/" + i.Key
			i.Event = "quota"
			items = append(items, i)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	if slices.Contains(c.Events, "certificate") {
		rows, e := s.DB.Query(ctx, `SELECT id,domain,CASE WHEN expires_at<now()+interval '14 days' THEN 'alert' ELSE 'ok' END FROM managed_certificates WHERE tenant_id=$1 AND expires_at IS NOT NULL LIMIT 200`, tenant)
		if e != nil {
			return e
		}
		for rows.Next() {
			var i item
			if e = rows.Scan(&i.Key, &i.Name, &i.State); e != nil {
				rows.Close()
				return e
			}
			i.Key = "certificate/" + i.Key
			i.Event = "certificate"
			items = append(items, i)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	sent := 0
	for _, i := range items {
		var old string
		_ = s.DB.QueryRow(ctx, `SELECT state FROM notification_state WHERE tenant_id=$1 AND resource=$2`, tenant, i.Key).Scan(&old)
		if old == i.State {
			continue
		}
		notify := i.State == "alert" || (i.State == "offline" || i.State == "online" && old != "") && slices.Contains(c.Events, i.Event)
		if notify {
			if sent >= 5 {
				continue
			}
			if !s.notificationCurrent(ctx, c) {
				return nil
			}
			text := fmt.Sprintf("星枢 · %s\n%s · %s", i.Event, i.Name, i.State)
			if i.Event == "quota" {
				text += fmt.Sprintf("\n用户本期已用达到 %d%% 阈值", c.QuotaPercent)
			}
			if i.Event == "certificate" {
				text += "\n证书不足 14 天，请检查续期"
			}
			e := s.sendTelegram(ctx, c, token, text)
			s.logNotification(ctx, i.Event, e)
			if e != nil {
				return e
			}
			sent++
		}
		if _, e := s.DB.Exec(ctx, `INSERT INTO notification_state(tenant_id,resource,state) VALUES($1,$2,$3) ON CONFLICT(tenant_id,resource) DO UPDATE SET state=excluded.state,updated_at=now()`, tenant, i.Key, i.State); e != nil {
			return e
		}
	}
	return nil
}
