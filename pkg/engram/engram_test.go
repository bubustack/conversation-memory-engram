package engram

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	cfgpkg "github.com/bubustack/conversation-memory-engram/pkg/config"
)

const helloText = "Hello."

func TestMaybeMerge_MergesWithinWindow(t *testing.T) {
	session := &sessionState{}
	now := time.Now()
	session.Messages = append(session.Messages, conversationMessage{Role: roleUser, Content: "I enjoyed."})
	session.LastRole = roleUser
	session.LastText = "I enjoyed."
	session.LastAt = now
	session.Updated = now

	merged := maybeMerge(session, roleUser, "The canyon.", now.Add(1*time.Second), 3*time.Second)
	if !merged {
		t.Fatal("expected merge to occur")
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected 1 merged message, got %d", len(session.Messages))
	}
	if session.Messages[0].Content != "I enjoyed. The canyon." {
		t.Fatalf("expected merged content, got %q", session.Messages[0].Content)
	}
}

func TestMaybeMerge_NoMergeOnRoleSwitch(t *testing.T) {
	session := &sessionState{}
	now := time.Now()
	session.Messages = append(session.Messages, conversationMessage{Role: roleUser, Content: helloText})
	session.LastRole = roleUser
	session.LastText = helloText
	session.LastAt = now

	merged := maybeMerge(session, roleAssistant, "Hi there!", now.Add(500*time.Millisecond), 3*time.Second)
	if merged {
		t.Fatal("should not merge different roles")
	}
}

func TestMaybeMerge_NoMergeOutsideWindow(t *testing.T) {
	session := &sessionState{}
	now := time.Now()
	session.Messages = append(session.Messages, conversationMessage{Role: roleUser, Content: helloText})
	session.LastRole = roleUser
	session.LastText = helloText
	session.LastAt = now

	merged := maybeMerge(session, roleUser, "World.", now.Add(5*time.Second), 3*time.Second)
	if merged {
		t.Fatal("should not merge outside window")
	}
}

func TestMaybeMerge_NoMergeZeroWindow(t *testing.T) {
	session := &sessionState{}
	now := time.Now()
	session.Messages = append(session.Messages, conversationMessage{Role: roleUser, Content: helloText})
	session.LastRole = roleUser
	session.LastText = helloText
	session.LastAt = now

	merged := maybeMerge(session, roleUser, "World.", now.Add(500*time.Millisecond), 0)
	if merged {
		t.Fatal("should not merge when window is 0")
	}
}

func TestMaybeMerge_NoMergeEmptySession(t *testing.T) {
	session := &sessionState{}
	merged := maybeMerge(session, roleUser, helloText, time.Now(), 3*time.Second)
	if merged {
		t.Fatal("should not merge into empty session")
	}
}

func TestCloneMetadataSkipsInvalidKeys(t *testing.T) {
	meta := map[string]string{
		"participant.id": "speaker-1",
		"":               "empty",
		" invalid ":      "whitespace",
	}

	cloned := cloneMetadata(meta)
	if got := cloned["participant.id"]; got != "speaker-1" {
		t.Fatalf("expected participant.id to be preserved, got %q", got)
	}
	if _, ok := cloned[""]; ok {
		t.Fatalf("expected empty metadata key to be dropped")
	}
	if _, ok := cloned[" invalid "]; ok {
		t.Fatalf("expected whitespace-padded metadata key to be dropped")
	}
}

func TestFirstNonEmptyBytesPrefersPayloadOverBinary(t *testing.T) {
	got := firstNonEmptyBytes(nil, []byte("payload"), []byte("binary"))
	if string(got) != "payload" {
		t.Fatalf("expected payload to win, got %q", got)
	}
}

func TestStartSweeperIsIdempotentForActiveStream(t *testing.T) {
	engine := New()
	engine.cfg = cfgpkg.Normalize(cfgpkg.Config{
		Sweep:      5 * time.Millisecond,
		SessionTTL: time.Minute,
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	engine.startSweeper(ctx, logger)
	engine.startSweeper(ctx, logger)

	engine.sweeperMu.Lock()
	launches := engine.sweeperLaunches
	running := engine.sweeperRunning
	engine.sweeperMu.Unlock()

	if !running {
		t.Fatal("expected sweeper to be running")
	}
	if launches != 1 {
		t.Fatalf("expected a single sweeper launch, got %d", launches)
	}

	cancel()
	time.Sleep(20 * time.Millisecond)

	engine.sweeperMu.Lock()
	running = engine.sweeperRunning
	engine.sweeperMu.Unlock()
	if running {
		t.Fatal("expected sweeper to stop after context cancellation")
	}
}
