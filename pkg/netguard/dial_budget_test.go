package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestGuardedDialContextSplitsDeadlineAcrossCandidates(t *testing.T) {
	t.Parallel()

	var deadlines []time.Time

	base := func(ctx context.Context, _, _ string) (net.Conn, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("dial attempt context has no deadline")
		}

		deadlines = append(deadlines, deadline)

		return nil, errors.New("attempt failed")
	}
	policy := Policy{Resolver: staticResolver{testExampleCom: {
		net.ParseIP("93.184.216.34"),
		net.ParseIP("93.184.216.35"),
		net.ParseIP("93.184.216.36"),
	}}}

	overall := 30 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), overall)

	defer cancel()

	overallDeadline, _ := ctx.Deadline()

	if _, err := GuardedDialContext(base, policy)(ctx, "tcp", "example.com:443"); err == nil {
		t.Fatal("guarded dial error = nil, want joined attempt errors")
	}

	if len(deadlines) != 3 {
		t.Fatalf("dial attempts = %d, want 3", len(deadlines))
	}

	for index, deadline := range deadlines[:2] {
		if !deadline.Before(overallDeadline) {
			t.Fatalf("attempt %d deadline = %v, want earlier than overall %v", index, deadline, overallDeadline)
		}

		if budget := time.Until(deadline); budget > overall/2 {
			t.Fatalf("attempt %d budget = %v, want at most half of %v", index, budget, overall)
		}
	}

	if !deadlines[2].Equal(overallDeadline) {
		t.Fatalf("last attempt deadline = %v, want overall deadline %v", deadlines[2], overallDeadline)
	}
}

func TestGuardedDialContextKeepsFullBudgetForSingleCandidate(t *testing.T) {
	t.Parallel()

	var attemptDeadline time.Time

	base := func(ctx context.Context, _, _ string) (net.Conn, error) {
		attemptDeadline, _ = ctx.Deadline()
		return nil, errors.New("attempt failed")
	}
	policy := Policy{Resolver: staticResolver{testExampleCom: {net.ParseIP("93.184.216.34")}}}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	overallDeadline, _ := ctx.Deadline()

	if _, err := GuardedDialContext(base, policy)(ctx, "tcp", "example.com:443"); err == nil {
		t.Fatal("guarded dial error = nil, want attempt error")
	}

	if !attemptDeadline.Equal(overallDeadline) {
		t.Fatalf("attempt deadline = %v, want unchanged overall deadline %v", attemptDeadline, overallDeadline)
	}
}

func TestGuardedDialContextStopsWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	attempts := 0
	base := func(context.Context, string, string) (net.Conn, error) {
		attempts++
		return nil, errors.New("attempt failed")
	}
	policy := Policy{Resolver: staticResolver{testExampleCom: {
		net.ParseIP("93.184.216.34"),
		net.ParseIP("93.184.216.35"),
	}}}

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := GuardedDialContext(base, policy)(ctx, "tcp", "example.com:443")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("guarded dial error = %v, want context.DeadlineExceeded", err)
	}

	if attempts != 0 {
		t.Fatalf("dial attempts = %d, want 0 once the budget is gone", attempts)
	}
}

func TestGuardedDialContextKeepsConnUsableAfterAttemptCancel(t *testing.T) {
	t.Parallel()

	port, accepted := listenLoopbackAccepting(t)

	requireRefusedLoopbackAddr(t, port)

	policy := Policy{
		Resolver: staticResolver{"probe.test": {
			net.ParseIP("127.0.0.2"),
			net.ParseIP("127.0.0.1"),
			net.ParseIP("127.0.0.3"),
		}},
		AllowedIPPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	dialer := &net.Dialer{}

	conn, err := GuardedDialContext(dialer.DialContext, policy)(ctx, "tcp", net.JoinHostPort("probe.test", port))
	if err != nil {
		t.Fatalf("guarded dial error = %v, want connection to the middle candidate", err)
	}

	defer testsupport.CloseNow(t, "conn.Close", conn.Close)

	server := <-accepted

	defer testsupport.CloseNow(t, "server.Close", server.Close)

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write after attempt cancel error = %v, want usable conn", err)
	}

	buf := make([]byte, 4)

	if err := server.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}

	if _, err := server.Read(buf); err != nil {
		t.Fatalf("server read error = %v, want data from a still-open conn", err)
	}

	if string(buf) != "ping" {
		t.Fatalf("server read = %q, want %q", buf, "ping")
	}
}

func listenLoopbackAccepting(t *testing.T) (string, <-chan net.Conn) {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listen unavailable: %v", err)
	}

	testsupport.CloseOnCleanup(t, "listener.Close", listener.Close)

	accepted := make(chan net.Conn, 1)

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		accepted <- conn
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}

	return port, accepted
}

func requireRefusedLoopbackAddr(t *testing.T, livePort string) {
	t.Helper()

	closedListener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listen unavailable: %v", err)
	}

	refusedHost, refusedPort, err := net.SplitHostPort(closedListener.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}

	testsupport.CloseNow(t, "closedListener.Close", closedListener.Close)

	if refusedHost != "127.0.0.1" || refusedPort == livePort {
		t.Skip("could not prepare a refused loopback address")
	}
}

func TestPolicyNormalizesAllowlistOnceAtGuardConstruction(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver:     staticResolver{"xn--bcher-kva.example": {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{" Bücher.Example. "},
	}

	prepared := policy.prepared()
	if len(prepared.normalizedAllowedHosts) != 1 || prepared.normalizedAllowedHosts[0] != "xn--bcher-kva.example" {
		t.Fatalf("prepared allowlist = %v, want normalized punycode form", prepared.normalizedAllowedHosts)
	}

	if policy.normalizedAllowedHosts != nil {
		t.Fatal("prepared() mutated the source policy")
	}

	dialed := 0
	base := func(_ context.Context, _, address string) (net.Conn, error) {
		dialed++

		if address != testValue931842 {
			t.Errorf("dialed address = %q, want resolved literal", address)
		}

		return nil, errors.New("stop after address capture")
	}
	dial := GuardedDialContext(base, policy)

	if err := callGuardedDial(dial, "xn--bcher-kva.example:443"); err == nil {
		t.Fatal("guarded dial error = nil, want captured dial error")
	}

	if dialed != 1 {
		t.Fatalf("dial attempts = %d, want 1", dialed)
	}

	// unicode 형태는 allowlist를 통과하고 resolve 단계에서 갈린다(ErrHostNotAllowed가 아니다).
	if err := callGuardedDial(dial, "bücher.example:443"); errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("guarded dial error = %v, want allowlist match for the unicode form", err)
	}

	if err := callGuardedDial(dial, "other.example:443"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("guarded dial error = %v, want ErrHostNotAllowed", err)
	}

	if _, err := policy.ValidateURL(t.Context(), "https://other.example/path"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("ValidateURL() error = %v, want ErrHostNotAllowed on the unprepared policy", err)
	}

	if _, err := policy.ValidateURL(t.Context(), "https://bücher.example/path"); err != nil &&
		!strings.Contains(err.Error(), "resolve host") {
		t.Fatalf("ValidateURL() error = %v, want allowlist match on the unprepared policy", err)
	}
}

func BenchmarkPolicyValidateHostAllowlist(b *testing.B) {
	policy := Policy{AllowedHosts: []string{
		"a.example", "b.example", "c.example", "bücher.example",
	}}.prepared()

	b.ReportAllocs()

	for b.Loop() {
		if err := policy.validateHost("bücher.example"); err != nil {
			b.Fatalf("validateHost() error = %v", err)
		}
	}
}
