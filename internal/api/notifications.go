package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"vps-control/internal/live"

	"github.com/jackc/pgx/v5"
)

type notificationConfig struct {
	Version         int      `json:"version"`
	Enabled         bool     `json:"enabled"`
	Token           string   `json:"bot_token,omitempty"`
	TokenConfigured bool     `json:"token_configured"`
	ChatID          string   `json:"chat_id"`
	CommandPolling  bool     `json:"command_polling"`
	AllowedUsers    []int64  `json:"allowed_user_ids"`
	Commands        []string `json:"commands"`
	Events          []string `json:"events"`
	ReportMinutes   int      `json:"report_minutes"`
	ReportSections  []string `json:"report_sections"`
	QuotaPercent    int      `json:"quota_percent"`
}

var botTokenPattern = regexp.MustCompile(`^[0-9]{5,20}:[A-Za-z0-9_-]{20,100}$`)
var chatIDPattern = regexp.MustCompile(`^-?[1-9][0-9]{0,18}$`)

func (c notificationConfig) validate() error {
	if !chatIDPattern.MatchString(c.ChatID) {
		return bad("Telegram chat_id must be a numeric chat ID")
	}
	if c.ReportMinutes != 0 && (c.ReportMinutes < 15 || c.ReportMinutes > 10080) {
		return bad("report interval must be 15-10080 minutes, or 0 to disable")
	}
	if c.QuotaPercent < 1 || c.QuotaPercent > 100 {
		return bad("quota threshold must be 1-100 percent")
	}
	if len(c.AllowedUsers) > 20 || len(c.Commands) > 5 || len(c.Events) > 4 {
		return bad("notification selection exceeds limits")
	}
	if c.CommandPolling && len(c.AllowedUsers) == 0 {
		return bad("command polling requires explicit allowed Telegram user IDs")
	}
	for _, id := range c.AllowedUsers {
		if id < 1 {
			return bad("invalid Telegram user ID")
		}
	}
	for _, v := range c.Commands {
		if !slices.Contains([]string{"status", "hosts", "traffic", "users", "help"}, v) {
			return bad("unsupported read-only command")
		}
	}
	if len(c.ReportSections) > 4 {
		return bad("too many report sections")
	}
	for _, v := range c.ReportSections {
		if !slices.Contains([]string{"status", "hosts", "traffic", "users"}, v) {
			return bad("unsupported report section")
		}
	}
	for _, v := range c.Events {
		if !slices.Contains([]string{"host_offline", "host_online", "quota", "certificate"}, v) {
			return bad("unsupported notification event")
		}
	}
	return nil
}
func (s *Server) loadNotifications(ctx context.Context) (notificationConfig, string, int64, error) {
	var c notificationConfig
	var raw, sealed []byte
	var offset int64
	err := s.DB.QueryRow(ctx, `SELECT settings,sealed,offset_id,version FROM notification_settings WHERE tenant_id=$1`, tenant).Scan(&raw, &sealed, &offset, &c.Version)
	if err != nil {
		return c, "", 0, err
	}
	version := c.Version
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, "", 0, err
	}
	c.Version = version
	if c.AllowedUsers == nil {
		c.AllowedUsers = []int64{}
	}
	if c.Commands == nil {
		c.Commands = []string{}
	}
	if c.Events == nil {
		c.Events = []string{}
	}
	if c.ReportSections == nil {
		c.ReportSections = []string{"status"}
	}
	plain, err := live.Open(s.MasterKey, sealed)
	c.TokenConfigured = err == nil && len(plain) > 0
	return c, string(plain), offset, err
}
func (s *Server) notifications(w http.ResponseWriter, r *http.Request) error {
	if e := s.liveReady(); e != nil {
		return e
	}
	if r.Method == "GET" {
		c, _, _, e := s.loadNotifications(r.Context())
		if e == pgx.ErrNoRows {
			c = notificationConfig{ReportMinutes: 60, QuotaPercent: 80, AllowedUsers: []int64{}, Commands: []string{"status", "help"}, Events: []string{}, ReportSections: []string{"status", "traffic"}}
			e = nil
		}
		if e != nil {
			return e
		}
		var lastPoll, lastSend *time.Time
		var lastError string
		_ = s.DB.QueryRow(r.Context(), `SELECT last_poll_at,last_send_at,last_error FROM notification_settings WHERE tenant_id=$1`, tenant).Scan(&lastPoll, &lastSend, &lastError)
		send(w, 200, map[string]any{"config": c, "last_poll_at": lastPoll, "last_send_at": lastSend, "last_error": lastError, "commands": []string{"status", "hosts", "traffic", "users", "help"}})
		return nil
	}
	if r.Method == "DELETE" {
		tx, e := s.DB.Begin(r.Context())
		if e != nil {
			return e
		}
		defer tx.Rollback(r.Context())
		if _, e = tx.Exec(r.Context(), `DELETE FROM notification_settings WHERE tenant_id=$1`, tenant); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `DELETE FROM notification_state WHERE tenant_id=$1`, tenant); e != nil {
			return e
		}
		if e = audit(r.Context(), tx, "admin", "notifications.unbind", "telegram"); e != nil {
			return e
		}
		if e = tx.Commit(r.Context()); e != nil {
			return e
		}
		send(w, 200, map[string]bool{"unbound": true})
		return nil
	}
	var c notificationConfig
	if e := decode(w, r, &c); e != nil {
		return e
	}
	if e := c.validate(); e != nil {
		return e
	}
	tx, e := s.DB.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(717013)`); e != nil {
		return e
	}
	var oldVersion int
	var sealed []byte
	e = tx.QueryRow(r.Context(), `SELECT version,sealed FROM notification_settings WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&oldVersion, &sealed)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if oldVersion != c.Version {
		return problem{409, "notification settings changed; refresh first"}
	}
	if c.Token != "" {
		if !botTokenPattern.MatchString(c.Token) {
			return bad("invalid Telegram bot token format")
		}
		sealed, e = live.Seal(s.MasterKey, []byte(c.Token))
		if e != nil {
			return e
		}
	}
	if len(sealed) == 0 {
		return bad("a bot token is required when first binding")
	}
	rotate := c.Token != ""
	c.Token = ""
	c.TokenConfigured = true
	c.Version = oldVersion + 1
	raw, _ := json.Marshal(c)
	_, e = tx.Exec(r.Context(), `INSERT INTO notification_settings(tenant_id,settings,sealed,next_report_at) VALUES($1,$2,$3,now()+($5::int*interval '1 minute')) ON CONFLICT(tenant_id) DO UPDATE SET settings=excluded.settings,sealed=excluded.sealed,version=notification_settings.version+1,offset_id=CASE WHEN $4 THEN 0 ELSE notification_settings.offset_id END,next_report_at=now()+($5::int*interval '1 minute'),next_alert_at=now(),last_error='',updated_at=now()`, tenant, raw, sealed, rotate, c.ReportMinutes)
	if e != nil {
		return e
	}
	if e = audit(r.Context(), tx, "admin", "notifications.settings", "telegram"); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	send(w, 200, c)
	return nil
}
func (s *Server) telegram(ctx context.Context, token, method string, body any, result any) error {
	raw, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(raw))
	if e != nil {
		return errors.New("Telegram request could not be created")
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.telegramClient
	if client == nil {
		client = &http.Client{Timeout: 28 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("Telegram network unavailable")
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
	if e != nil || len(b) > 256<<10 {
		return errors.New("Telegram response invalid")
	}
	var envelope struct {
		OK        bool            `json:"ok"`
		Result    json.RawMessage `json:"result"`
		ErrorCode int             `json:"error_code"`
	}
	if json.Unmarshal(b, &envelope) != nil || !envelope.OK || res.StatusCode != 200 {
		return fmt.Errorf("Telegram API rejected request (HTTP %d, code %d); check token/chat, bot start and webhook conflicts", res.StatusCode, envelope.ErrorCode)
	}
	if result != nil && json.Unmarshal(envelope.Result, result) != nil {
		return errors.New("Telegram result invalid")
	}
	return nil
}
func (s *Server) sendTelegram(ctx context.Context, c notificationConfig, token, text string) error {
	if len([]rune(text)) > 3900 {
		text = string([]rune(text)[:3900]) + "…"
	}
	return s.telegram(ctx, token, "sendMessage", map[string]any{"chat_id": c.ChatID, "text": text, "disable_notification": false, "link_preview_options": map[string]bool{"is_disabled": true}}, nil)
}
func (s *Server) notificationSummary(ctx context.Context, command string) (string, error) {
	switch command {
	case "status":
		var hosts, online, services, running int
		e := s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL),(SELECT count(*) FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL AND last_seen>now()-interval '90 seconds'),(SELECT count(*) FROM live_services WHERE tenant_id=$1),(SELECT count(*) FROM live_services WHERE tenant_id=$1 AND enabled AND state='running' AND reported_version=version AND reported_at>now()-interval '45 seconds')`, tenant).Scan(&hosts, &online, &services, &running)
		return fmt.Sprintf("星枢 · 状态\n在线主机 %d/%d\n正常运行入站 %d/%d", online, hosts, running, services), e
	case "traffic":
		var used, quota int64
		var users int
		e := s.DB.QueryRow(ctx, `SELECT COALESCE(sum(used_bytes),0),COALESCE(sum(quota_bytes),0),count(*) FROM proxy_users WHERE tenant_id=$1`, tenant).Scan(&used, &quota, &users)
		return fmt.Sprintf("星枢 · 用户流量\n用户 %d\n本期合计 %.2f GiB\n有限额度合计 %.2f GiB\n共享密钥入口不计入个人账单", users, float64(used)/(1<<30), float64(quota)/(1<<30)), e
	case "hosts", "users":
		query := `SELECT name,CASE WHEN last_seen>now()-interval '90 seconds' THEN '在线' ELSE '离线/未接入' END FROM hosts WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name LIMIT 30`
		if command == "users" {
			query = `SELECT name,round(used_bytes::numeric/1073741824,2)::text||' GiB / '||CASE WHEN quota_bytes=0 THEN '不限额' ELSE round(quota_bytes::numeric/1073741824,2)::text||' GiB' END FROM proxy_users WHERE tenant_id=$1 ORDER BY name LIMIT 30`
		}
		rows, e := s.DB.Query(ctx, query, tenant)
		if e != nil {
			return "", e
		}
		defer rows.Close()
		lines := []string{"星枢 · " + command + "（最多 30 条）"}
		for rows.Next() {
			var name, state string
			if e = rows.Scan(&name, &state); e != nil {
				return "", e
			}
			lines = append(lines, name+" · "+state)
		}
		return strings.Join(lines, "\n"), rows.Err()
	default:
		return "星枢只读查询：/status /hosts /traffic /users /help\n可用命令取决于面板授权；不提供启停、删除、Shell、凭据或订阅链接查询。", nil
	}
}
func (s *Server) notificationTest(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Preview bool `json:"preview"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	c, token, _, e := s.loadNotifications(r.Context())
	if e != nil {
		return e
	}
	text, e := s.notificationSummary(r.Context(), "status")
	if e != nil {
		return e
	}
	if !b.Preview {
		if !c.Enabled {
			return bad("enable the binding before sending a test")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		e = s.sendTelegram(ctx, c, token, "连接测试\n"+text)
		s.logNotification(ctx, "test", e)
		if e != nil {
			return bad(e.Error())
		}
	}
	send(w, 200, map[string]any{"preview": b.Preview, "message": text, "sent": !b.Preview})
	return nil
}
func (s *Server) notificationLog(w http.ResponseWriter, r *http.Request) error {
	var raw json.RawMessage
	e := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT id,event,state,detail,created_at FROM notification_log WHERE tenant_id=$1 ORDER BY id DESC LIMIT 100) x`, tenant).Scan(&raw)
	if e != nil {
		return e
	}
	send(w, 200, raw)
	return nil
}

type telegramUpdate struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Text string `json:"text"`
		Date int64  `json:"date"`
		From struct {
			ID  int64 `json:"id"`
			Bot bool  `json:"is_bot"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func allowedTelegramCommand(c notificationConfig, u telegramUpdate, now time.Time) string {
	m := u.Message
	if m == nil || m.From.Bot || strconv.FormatInt(m.Chat.ID, 10) != c.ChatID || !slices.Contains(c.AllowedUsers, m.From.ID) || m.Date < now.Add(-2*time.Minute).Unix() || m.Date > now.Add(time.Minute).Unix() {
		return ""
	}
	parts := strings.Fields(m.Text)
	if len(parts) != 1 {
		return ""
	}
	cmd := strings.Split(strings.TrimPrefix(parts[0], "/"), "@")[0]
	if !strings.HasPrefix(parts[0], "/") || !slices.Contains(c.Commands, cmd) {
		return ""
	}
	return cmd
}
