package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

type telegramAPIError struct{ Code, RetryAfter int }

func (e telegramAPIError) Error() string {
	reason := map[int]string{400: "聊天或请求参数无效，请检查聊天 ID", 401: "机器人令牌无效", 403: "机器人被屏蔽或尚未开始聊天", 409: "Webhook 或另一个命令轮询程序冲突", 429: "Telegram 限流，稍后自动重试"}[e.Code]
	if reason == "" {
		reason = "Telegram 服务暂时不可用"
	}
	return fmt.Sprintf("Telegram %d：%s", e.Code, reason)
}

func (s *Server) notificationHealth(ctx context.Context, c notificationConfig) map[string]any {
	var poll, sent, command, ignored, nextReport *time.Time
	var pollError, sendError, lastCommand, ignoredReason, username string
	var skew int
	_ = s.DB.QueryRow(ctx, `SELECT last_poll_at,last_send_at,last_poll_error,last_send_error,last_command_at,last_command,last_ignored_at,last_ignored_reason,bot_username,next_report_at,telegram_clock_skew_seconds FROM notification_settings WHERE tenant_id=$1`, tenant).Scan(&poll, &sent, &pollError, &sendError, &command, &lastCommand, &ignored, &ignoredReason, &username, &nextReport, &skew)
	state := "disabled"
	hints := []string{}
	if c.Enabled && c.CommandPolling {
		state = "starting"
		if pollError != "" {
			state = "error"
		} else if poll != nil && time.Since(*poll) < 90*time.Second {
			state = "receiving"
		} else if poll != nil {
			state = "stale"
		}
	} else if c.Enabled {
		hints = append(hints, "通知发送已开启，命令接收未开启；测试发送成功不代表命令可用")
	}
	if c.CommandPolling && len(c.AllowedUsers) == 0 {
		hints = append(hints, "请授权查询用户 ID")
	}
	if c.CommandPolling && len(c.Commands) == 0 {
		hints = append(hints, "尚未选择允许执行的命令")
	}
	owner, e := strconv.ParseInt(c.ChatID, 10, 64)
	return map[string]any{"poll_state": state, "last_poll_error": pollError, "last_send_error": sendError, "last_command_at": command, "last_command": lastCommand, "last_ignored_at": ignored, "last_ignored_reason": ignoredReason, "bot_username": username, "next_report_at": nextReport, "hints": hints, "clock_skew_seconds": skew, "private_owner_can_enable": e == nil && owner > 0, "private_owner_allowed": slices.Contains(c.AllowedUsers, owner)}
}

// Diagnosis never consumes updates or modifies an existing webhook.
func (s *Server) notificationDiagnose(w http.ResponseWriter, r *http.Request) error {
	var body struct{}
	if e := decode(w, r, &body); e != nil {
		return e
	}
	c, token, _, e := s.loadNotifications(r.Context())
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()
	var bot struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Name     string `json:"first_name"`
	}
	if e = s.telegram(ctx, token, "getMe", map[string]any{}, &bot); e != nil {
		return bad(e.Error())
	}
	var webhook struct {
		URL     string `json:"url"`
		Pending int    `json:"pending_update_count"`
	}
	if e = s.telegram(ctx, token, "getWebhookInfo", map[string]any{}, &webhook); e != nil {
		return bad(e.Error())
	}
	var menu []struct {
		Command string `json:"command"`
	}
	if e = s.telegram(ctx, token, "getMyCommands", map[string]any{"scope": map[string]any{"type": "chat", "chat_id": c.ChatID}}, &menu); e != nil {
		return bad(e.Error())
	}
	_, e = s.DB.Exec(ctx, `UPDATE notification_settings SET bot_username=$2 WHERE tenant_id=$1 AND version=$3`, tenant, bot.Username, c.Version)
	if e != nil {
		return e
	}
	hints := []string{}
	if webhook.URL != "" {
		hints = append(hints, "机器人已绑定 Webhook，命令长轮询不可用；先确认原有服务用途，本平台不会自动删除 Webhook")
	}
	if !c.CommandPolling {
		hints = append(hints, "命令接收未开启")
	}
	if len(c.AllowedUsers) == 0 {
		hints = append(hints, "未授权任何查询用户")
	}
	commands := []string{}
	for _, m := range menu {
		commands = append(commands, m.Command)
	}
	send(w, 200, map[string]any{"bot_username": bot.Username, "bot_name": bot.Name, "webhook_present": webhook.URL != "", "pending_updates": webhook.Pending, "menu_commands": commands, "allowed_commands": c.Commands, "hints": hints, "checked_at": time.Now(), "notice": "诊断未读取或消耗聊天消息；菜单设置与命令授权、接收状态分别生效"})
	return nil
}

func (s *Server) notificationCommands(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Version int `json:"version"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	c, token, _, e := s.loadNotifications(r.Context())
	if e != nil {
		return e
	}
	if c.Version != b.Version {
		return problem{409, "notification settings changed; refresh first"}
	}
	commands := []map[string]string{}
	names := map[string]string{"status": "状态概览", "hosts": "主机列表", "traffic": "流量与剩余额度", "users": "用户额度", "help": "查看可用命令"}
	for _, cmd := range c.Commands {
		commands = append(commands, map[string]string{"command": cmd, "description": names[cmd]})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()
	if e = s.telegram(ctx, token, "setMyCommands", map[string]any{"commands": commands, "scope": map[string]any{"type": "chat", "chat_id": c.ChatID}}, nil); e != nil {
		return bad(e.Error())
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = audit(ctx, tx, "admin", "notifications.commands-sync", "telegram"); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	send(w, 200, map[string]any{"synced": true, "scope": "bound_chat", "commands": c.Commands})
	return nil
}

func (s *Server) logIgnoredCommand(ctx context.Context, reason string) {
	if reason != "" {
		_, _ = s.DB.Exec(ctx, `UPDATE notification_settings SET last_ignored_at=now(),last_ignored_reason=$2 WHERE tenant_id=$1`, tenant, reason)
	}
}

// Keep poll failures independent from successful outbound notification sends.
func (s *Server) notificationPollingState(ctx context.Context, e error) {
	message := ""
	if e != nil {
		message = e.Error()
	}
	_, _ = s.DB.Exec(ctx, `UPDATE notification_settings SET last_poll_error=$2,last_error=CASE WHEN $2<>'' THEN $2 ELSE last_send_error END,last_poll_at=CASE WHEN $2='' THEN now() ELSE last_poll_at END WHERE tenant_id=$1`, tenant, message)
}
