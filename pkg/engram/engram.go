package engram

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	sdk "github.com/bubustack/bubu-sdk-go"
	sdkengram "github.com/bubustack/bubu-sdk-go/engram"
	cfgpkg "github.com/bubustack/conversation-memory-engram/pkg/config"
	"github.com/bubustack/tractatus/transport"
)

const (
	componentName = "conversation-memory-engram"
	contextType   = "conversation.context.v1"
	roleAssistant = "assistant"
	roleUser      = "user"
)

type ConversationMemory struct {
	cfg      cfgpkg.Config
	ignoreRE *regexp.Regexp
	mu       sync.RWMutex
	sessions map[string]*sessionState

	sweeperMu       sync.Mutex
	sweeperRunning  bool
	sweeperLaunches int
}

type sessionState struct {
	Messages []conversationMessage
	LastRole string
	LastText string
	LastAt   time.Time
	Updated  time.Time
	// Last mirrored assistant reply injected via input.assistantText.
	LastMirroredAssistant string
}

type conversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamInput struct {
	Key            string `json:"key,omitempty"`
	SessionID      string `json:"sessionId,omitempty"`
	Role           string `json:"role,omitempty"`
	Text           string `json:"text,omitempty"`
	AssistantText  string `json:"assistantText,omitempty"`
	SpeakerID      string `json:"speakerId,omitempty"`
	Reset          bool   `json:"reset,omitempty"`
	MaxMessages    int    `json:"maxMessages,omitempty"`
	MinUserChars   int    `json:"minUserChars,omitempty"`
	IncludeHistory *bool  `json:"includeHistory,omitempty"`
}

func New() *ConversationMemory {
	return &ConversationMemory{
		sessions: make(map[string]*sessionState),
	}
}

func (e *ConversationMemory) Init(_ context.Context, cfg cfgpkg.Config, _ *sdkengram.Secrets) error {
	normalized := cfgpkg.Normalize(cfg)
	compiled, err := regexp.Compile(normalized.IgnorePattern)
	if err != nil {
		return err
	}
	e.cfg = normalized
	e.ignoreRE = compiled
	if e.sessions == nil {
		e.sessions = make(map[string]*sessionState)
	}
	return nil
}

func (e *ConversationMemory) Stream(
	ctx context.Context,
	in <-chan sdkengram.InboundMessage,
	out chan<- sdkengram.StreamMessage,
) error {
	logger := sdk.LoggerFromContext(ctx).With(
		"component", componentName,
		"mode", "stream",
	)
	e.startSweeper(ctx, logger)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-in:
			if !ok {
				return nil
			}
			if isHeartbeat(msg.Metadata) {
				msg.Done()
				continue
			}
			if err := e.processMessage(ctx, msg, out, logger); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				logger.Warn("Failed to process memory packet", "error", err)
				msg.Done()
				continue
			}
			msg.Done()
		}
	}
}

func (e *ConversationMemory) processMessage(
	ctx context.Context,
	msg sdkengram.InboundMessage,
	out chan<- sdkengram.StreamMessage,
	logger *slog.Logger,
) error {
	raw := firstNonEmptyBytes(msg.Inputs, msg.Payload, binaryPayload(msg))
	if len(raw) == 0 {
		return nil
	}

	var in streamInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}

	// Best-effort parse into generic map for fallback field extraction (text, userPrompt).
	// The typed unmarshal above already validated JSON syntax, so this cannot fail in practice.
	parsed := make(map[string]any)
	_ = json.Unmarshal(raw, &parsed) //nolint:errcheck // intentional: fallback extraction

	key := e.resolveKey(&in, msg.Metadata)
	role := resolveRole(&in, msg.Metadata, e.cfg.DefaultRole)
	text := strings.TrimSpace(firstNonEmptyString(in.Text, stringField(parsed, "text"), stringField(parsed, "userPrompt")))
	assistantText := strings.TrimSpace(firstNonEmptyString(in.AssistantText, stringField(parsed, "assistantText")))

	if in.Reset {
		e.resetSession(key)
	}

	maxMessages := e.cfg.MaxMessages
	if in.MaxMessages > 0 {
		maxMessages = in.MaxMessages
	}
	minChars := e.cfg.MinUserChars
	if in.MinUserChars > 0 {
		minChars = in.MinUserChars
	}

	accepted, reason, history := e.applyMessage(key, role, text, assistantText, maxMessages, minChars, time.Now().UTC())

	includeHistory := true
	if in.IncludeHistory != nil {
		includeHistory = *in.IncludeHistory
	}

	output := map[string]any{
		"type":         contextType,
		"key":          key,
		"accepted":     accepted,
		"reason":       reason,
		"role":         role,
		"text":         text,
		"speakerId":    firstNonEmptyString(in.SpeakerID, participantIdentityFromMetadata(msg.Metadata)),
		"messageCount": len(history),
	}
	if includeHistory {
		output["history"] = history
	}

	payloadBytes, err := json.Marshal(output)
	if err != nil {
		return err
	}

	metadata := cloneMetadata(msg.Metadata)
	metadata["provider"] = "conversation-memory"
	metadata["type"] = contextType

	if accepted {
		logger.Info("Conversation memory updated",
			"key", key,
			"role", role,
			"messageCount", len(history),
			"textPreview", truncateText(text, 120),
		)
	} else if reason != "" {
		logger.Debug("Conversation memory skipped input",
			"key", key,
			"role", role,
			"reason", reason,
			"textPreview", truncateText(text, 120),
		)
	}

	select {
	case out <- sdkengram.StreamMessage{
		Metadata: metadata,
		Inputs:   payloadBytes,
		Payload:  payloadBytes,
	}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *ConversationMemory) startSweeper(ctx context.Context, logger *slog.Logger) {
	e.sweeperMu.Lock()
	if e.sweeperRunning {
		e.sweeperMu.Unlock()
		return
	}
	e.sweeperRunning = true
	e.sweeperLaunches++
	e.sweeperMu.Unlock()

	ticker := time.NewTicker(e.cfg.Sweep)
	go func() {
		defer ticker.Stop()
		defer func() {
			e.sweeperMu.Lock()
			e.sweeperRunning = false
			e.sweeperMu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed := e.gcExpired(time.Now().UTC())
				if removed > 0 {
					logger.Info("Conversation memory sweep completed", "removedSessions", removed)
				}
			}
		}
	}()
}

func (e *ConversationMemory) gcExpired(now time.Time) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.sessions) == 0 {
		return 0
	}
	removed := 0
	for key, session := range e.sessions {
		if now.Sub(session.Updated) > e.cfg.SessionTTL {
			delete(e.sessions, key)
			removed++
		}
	}
	return removed
}

func (e *ConversationMemory) resetSession(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sessions, key)
}

func (e *ConversationMemory) applyMessage(
	key, role, text, assistantText string,
	maxMessages, minChars int,
	now time.Time,
) (bool, string, []conversationMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()

	session := e.ensureSessionLocked(key, now)
	e.maybeMirrorAssistantLocked(session, role, text, assistantText, maxMessages, now)

	if reason := e.rejectionReasonLocked(session, role, text, minChars, now); reason != "" {
		return false, reason, cloneHistory(session.Messages)
	}

	// Try merging with previous message from same role within merge window
	if maybeMerge(session, role, text, now, e.cfg.MergeWindow) {
		return true, "merged", cloneHistory(session.Messages)
	}

	recordSessionMessage(session, role, text, maxMessages, now)

	return true, "", cloneHistory(session.Messages)
}

func (e *ConversationMemory) ensureSessionLocked(key string, now time.Time) *sessionState {
	session := e.sessions[key]
	if session == nil {
		session = &sessionState{Updated: now}
		e.sessions[key] = session
	}
	session.Updated = now
	return session
}

func (e *ConversationMemory) maybeMirrorAssistantLocked(
	session *sessionState,
	role, text, assistantText string,
	maxMessages int,
	now time.Time,
) {
	// In split-step topologies, assistant replies may be supplied as a mirrored field
	// on user packets so a single in-memory session can retain both sides.
	if role != roleUser || text == "" {
		return
	}
	mirrored := strings.TrimSpace(assistantText)
	if mirrored == "" || strings.EqualFold(mirrored, session.LastMirroredAssistant) {
		return
	}
	recordSessionMessage(session, roleAssistant, mirrored, maxMessages, now)
	session.LastMirroredAssistant = mirrored
}

func (e *ConversationMemory) rejectionReasonLocked(
	session *sessionState,
	role, text string,
	minChars int,
	now time.Time,
) string {
	if text == "" {
		return "empty_text"
	}
	if role == roleUser {
		if utf8.RuneCountInString(text) < minChars {
			return "short_user_text"
		}
		if e.ignoreRE != nil && e.ignoreRE.MatchString(text) {
			return "ignored_pattern"
		}
	}
	if e.cfg.DedupeWindow <= 0 {
		return ""
	}
	if session.LastRole != role || !strings.EqualFold(session.LastText, text) {
		return ""
	}
	if session.LastAt.IsZero() || now.Sub(session.LastAt) > e.cfg.DedupeWindow {
		return ""
	}
	return "duplicate_recent"
}

func recordSessionMessage(
	session *sessionState,
	role, text string,
	maxMessages int,
	now time.Time,
) {
	session.Messages = append(session.Messages, conversationMessage{
		Role:    role,
		Content: text,
	})
	session.Messages = trimMessages(session.Messages, maxMessages)
	session.LastRole = role
	session.LastText = text
	if role == roleAssistant {
		session.LastMirroredAssistant = text
	}
	session.LastAt = now
	session.Updated = now
}

func trimMessages(messages []conversationMessage, maxMessages int) []conversationMessage {
	if maxMessages <= 0 || len(messages) <= maxMessages {
		return messages
	}
	return cloneHistory(messages[len(messages)-maxMessages:])
}

// maybeMerge concatenates text to the last message if same role and within the
// merge window. Returns true if the merge occurred.
func maybeMerge(session *sessionState, role, text string, now time.Time, window time.Duration) bool {
	if window <= 0 {
		return false
	}
	if len(session.Messages) == 0 {
		return false
	}
	if session.LastRole != role {
		return false
	}
	if session.LastAt.IsZero() || now.Sub(session.LastAt) > window {
		return false
	}
	// Merge: append to last message
	last := &session.Messages[len(session.Messages)-1]
	last.Content = last.Content + " " + text
	session.LastText = last.Content
	session.LastAt = now
	session.Updated = now
	return true
}

func (e *ConversationMemory) resolveKey(in *streamInput, meta map[string]string) string {
	if in != nil && strings.TrimSpace(in.Key) != "" {
		return strings.TrimSpace(in.Key)
	}
	sessionID := ""
	if in != nil {
		sessionID = strings.TrimSpace(in.SessionID)
	}
	if sessionID == "" {
		sessionID = firstNonEmptyString(
			meta["sessionId"],
			meta["session.id"],
			meta["hook.sessionId"],
			meta["storyrun-name"],
			meta["storyrun.id"],
		)
	}
	participant := ""
	if in != nil {
		participant = strings.TrimSpace(in.SpeakerID)
	}
	if participant == "" {
		participant = participantIdentityFromMetadata(meta)
	}
	if sessionID != "" && participant != "" {
		return sessionID + ":" + participant
	}
	if sessionID != "" {
		return sessionID
	}
	if participant != "" {
		return "participant:" + participant
	}
	return "default"
}

func resolveRole(in *streamInput, meta map[string]string, defaultRole string) string {
	if in != nil {
		if role := normalizeRole(in.Role); role != "" {
			return role
		}
	}
	typeValue := strings.ToLower(strings.TrimSpace(meta["type"]))
	switch {
	case strings.HasPrefix(typeValue, "speech.transcript"),
		strings.HasPrefix(typeValue, transport.StreamTypeSpeechTranslation):
		return roleUser
	case strings.HasPrefix(typeValue, "openai.chat"):
		return roleAssistant
	default:
		return normalizeRole(defaultRole)
	}
}

func normalizeRole(raw string) string {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case roleUser, roleAssistant, "system":
		return role
	case "developer":
		return "system"
	default:
		return ""
	}
}

func participantIdentityFromMetadata(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	keys := []string{
		"participant.id",
		"participant.identity",
		"participant",
		"participant_identity",
		"participant-id",
		"participantId",
		"identity",
		"livekit-participant",
		"livekit.participant",
		"speaker",
		"speaker.id",
	}
	for _, key := range keys {
		if value := strings.TrimSpace(meta[key]); value != "" {
			return value
		}
	}
	for key, value := range meta {
		if strings.Contains(strings.ToLower(key), "participant") && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringField(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func cloneMetadata(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(meta))
	for key, value := range meta {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" || trimmedKey != key {
			continue
		}
		out[key] = value
	}
	return out
}

func cloneHistory(in []conversationMessage) []conversationMessage {
	if len(in) == 0 {
		return []conversationMessage{}
	}
	out := make([]conversationMessage, len(in))
	copy(out, in)
	return out
}

func firstNonEmptyBytes(values ...[]byte) []byte {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func binaryPayload(msg sdkengram.InboundMessage) []byte {
	if msg.Binary == nil {
		return nil
	}
	return msg.Binary.Payload
}

func isHeartbeat(meta map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(meta["bubu-heartbeat"]), "true")
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
