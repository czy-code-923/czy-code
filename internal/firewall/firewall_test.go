package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func sample() Export {
	return Export{HostID: strings.Repeat("a", 32), Policy: Policy{Name: "Secure ingress", SSHPort: 22, AdminCIDRs: []string{"192.0.2.0/24", "2001:db8::/64"}, DefaultAction: "drop", Rules: []Rule{{Name: "proxy", Protocol: "tcp", Port: 2080, Source: "0.0.0.0/0", Action: "accept"}}}}
}
func TestCompileProtectsManagement(t *testing.T) {
	p := sample()
	body, e := Compile(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, guard := range []string{"iifname \"lo\" accept", "ct state established,related accept", "192.0.2.0/24 tcp dport 22 accept", "2001:db8::/64 tcp dport 22 accept", "tcp dport { 8080, 5173 } drop", "tcp dport 22 drop", "ipv6-icmp accept"} {
		if !strings.Contains(body, guard) {
			t.Fatal("missing", guard)
		}
	}
	if strings.Contains(body, "flush ruleset") {
		t.Fatal("global ruleset modified")
	}
	if !p.Policy.AllowsPeer("192.0.2.10") || p.Policy.AllowsPeer("203.0.113.10") {
		t.Fatal("peer guard")
	}
}

func TestOwnedRemovalRequiresConfirmationAndCanRollback(t *testing.T) {
	ctx := context.Background()
	b := &fakeBackend{current: "original owned table", exists: true}
	scheduler := &fakeScheduler{backend: b}
	m, e := OpenManager(t.TempDir(), b, scheduler)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	r, e := m.StartRemoval(ctx, sample().HostID, time.Minute)
	if e != nil || !scheduler.armed || b.exists {
		t.Fatal(r, e, b.exists)
	}
	if e = m.Rollback(ctx, r.ID); e != nil || !b.exists || !strings.Contains(b.current, "original owned table") {
		t.Fatal("removal did not restore owned snapshot", e)
	}
}
func TestRejectUnsafePolicy(t *testing.T) {
	for _, name := range []string{"admin-public", "injection", "protected-port", "ssh-web", "duplicate", "range", "noncanonical"} {
		t.Run(name, func(t *testing.T) {
			p := sample()
			switch name {
			case "admin-public":
				p.Policy.AdminCIDRs = []string{"0.0.0.0/0"}
			case "injection":
				p.Policy.Rules[0].Source = "0.0.0.0/0; flush ruleset"
			case "protected-port":
				p.Policy.Rules[0].Port = 8080
			case "ssh-web":
				p.Policy.SSHPort = 5173
			case "duplicate":
				p.Policy.Rules = append(p.Policy.Rules, p.Policy.Rules[0])
			case "range":
				p.Policy.Rules[0].Port = 1
				p.Policy.Rules[0].EndPort = 65535
			case "noncanonical":
				p.Policy.Rules[0].Source = "192.0.2.3/24"
			}
			if _, e := Compile(p); e == nil {
				t.Fatal("unsafe policy accepted")
			}
		})
	}
}

type fakeBackend struct {
	current   string
	exists    bool
	failApply bool
	calls     int
}

func (b *fakeBackend) Snapshot(context.Context, string) (string, bool, error) {
	return b.current, b.exists, nil
}
func (b *fakeBackend) Check(context.Context, string) error { return nil }
func (b *fakeBackend) Apply(_ context.Context, s string) error {
	b.calls++
	if b.failApply {
		b.failApply = false
		return errors.New("failure")
	}
	b.current = s
	b.exists = !strings.HasSuffix(strings.TrimSpace(s), Table(sample().HostID))
	return nil
}

type fakeScheduler struct {
	armed   bool
	fail    bool
	backend *fakeBackend
}

func (s *fakeScheduler) Arm(context.Context, string, time.Duration) error {
	if s.backend.calls != 0 {
		return errors.New("write before arming")
	}
	if s.fail {
		return errors.New("timer failure")
	}
	s.armed = true
	return nil
}
func TestGuardedApplyAndRollback(t *testing.T) {
	ctx := context.Background()
	b := &fakeBackend{current: "original owned table", exists: true}
	scheduler := &fakeScheduler{backend: b}
	m, e := OpenManager(t.TempDir(), b, scheduler)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	r, e := m.Start(ctx, sample(), "192.0.2.10", 60*time.Second)
	if e != nil || !scheduler.armed || b.calls != 1 {
		t.Fatal(r, e)
	}
	if _, e = m.Start(ctx, sample(), "192.0.2.10", 60*time.Second); e == nil {
		t.Fatal("parallel pending apply allowed")
	}
	if e = m.Confirm(ctx, r.ID, "wrong"); e == nil {
		t.Fatal("wrong token")
	}
	if e = m.Rollback(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	j, _ := m.read(r.ID)
	if j.Status != "rolled_back" || !strings.Contains(b.current, "original owned table") {
		t.Fatal(j, b.current)
	}
}
func TestTimerFailureNeverChangesKernel(t *testing.T) {
	b := &fakeBackend{}
	s := &fakeScheduler{fail: true, backend: b}
	m, _ := OpenManager(t.TempDir(), b, s)
	defer m.Close()
	if _, e := m.Start(context.Background(), sample(), "192.0.2.10", time.Minute); e == nil || b.calls != 0 {
		t.Fatal("unsafe timer failure", e)
	}
	if _, e := m.Start(context.Background(), sample(), "203.0.113.1", time.Minute); e == nil || b.calls != 0 {
		t.Fatal("unprotected peer accepted")
	}
}
func TestConfirmedTimerBecomesNoop(t *testing.T) {
	b := &fakeBackend{}
	m, _ := OpenManager(t.TempDir(), b, &fakeScheduler{backend: b})
	defer m.Close()
	r, e := m.Start(context.Background(), sample(), "192.0.2.10", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Confirm(context.Background(), r.ID, r.ConfirmToken); e != nil {
		t.Fatal(e)
	}
	if e = m.Rollback(context.Background(), r.ID); e != nil || b.calls != 1 {
		t.Fatal("confirmed rules rolled back", e)
	}
}
func TestApplyFailureRestoresAndExpiredConfirmRefused(t *testing.T) {
	ctx := context.Background()
	b := &fakeBackend{current: "original owned table", exists: true, failApply: true}
	m, _ := OpenManager(t.TempDir(), b, &fakeScheduler{backend: b})
	defer m.Close()
	if _, e := m.Start(ctx, sample(), "192.0.2.10", time.Minute); e == nil || b.calls != 2 || !strings.Contains(b.current, "original owned table") {
		t.Fatal("failed apply not restored", e, b.calls)
	}
	b2 := &fakeBackend{}
	m2, _ := OpenManager(t.TempDir(), b2, &fakeScheduler{backend: b2})
	defer m2.Close()
	r, e := m2.Start(ctx, sample(), "192.0.2.10", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	j, _ := m2.read(r.ID)
	j.Deadline = time.Now().Add(-time.Second)
	m2.save(j)
	if e = m2.Confirm(ctx, r.ID, r.ConfirmToken); e == nil {
		t.Fatal("expired confirmation accepted")
	}
	if e = m2.Rollback(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if b2.exists {
		t.Fatal("new table not removed on rollback")
	}
}
