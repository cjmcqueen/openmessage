package app

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// Google send diagnostics.
//
// Sends to a brand-new RCS group have come back FAILURE_3 while a send from
// the web UI to the same group minutes later succeeded, and the failing paths
// logged nothing about the request. LogGoogleTextSend records, for every
// Google text send, the conversation it targeted, how the request was built,
// and Google's answer, so the next failure can be compared with a success.
// Message bodies are never logged.

// googleGroupCreatedTTL bounds how long a creation time is kept. The question
// is how soon after creation a group accepts sends, so an hour is plenty.
const googleGroupCreatedTTL = time.Hour

var googleGroupsCreated = struct {
	sync.Mutex
	at  map[string]time.Time
	now func() time.Time
}{now: time.Now}

// NoteGoogleGroupCreated records that this process just created the group
// conversationID, so later sends to it can log the group's age.
func NoteGoogleGroupCreated(conversationID string) {
	if conversationID == "" {
		return
	}
	googleGroupsCreated.Lock()
	defer googleGroupsCreated.Unlock()
	now := googleGroupsCreated.now()
	if googleGroupsCreated.at == nil {
		googleGroupsCreated.at = make(map[string]time.Time)
	}
	for id, at := range googleGroupsCreated.at {
		if now.Sub(at) > googleGroupCreatedTTL {
			delete(googleGroupsCreated.at, id)
		}
	}
	googleGroupsCreated.at[conversationID] = now
}

// googleGroupAge reports how long ago this process created conversationID,
// if it did so within googleGroupCreatedTTL.
func googleGroupAge(conversationID string) (time.Duration, bool) {
	googleGroupsCreated.Lock()
	defer googleGroupsCreated.Unlock()
	at, ok := googleGroupsCreated.at[conversationID]
	if !ok {
		return 0, false
	}
	age := googleGroupsCreated.now().Sub(at)
	if age > googleGroupCreatedTTL {
		return 0, false
	}
	return age, true
}

// LogGoogleTextSend logs one Google text send. source names the caller
// ("web", "mcp:send_group_message", ...). conv is the conversation the
// payload was built from and may be nil. Pass the SendMessage error, if any;
// resp is ignored when sendErr is set. Successes log at info, failures at warn.
func LogGoogleTextSend(logger zerolog.Logger, source string, conv *gmproto.Conversation, payload *gmproto.SendMessageRequest, resp *gmproto.SendMessageResponse, sendErr error) {
	failed := sendErr != nil || resp.GetStatus() != gmproto.SendMessageResponse_SUCCESS
	ev := logger.Info()
	if failed {
		ev = logger.Warn()
	}
	ev = ev.Str("source", source).
		Str("conv_id", payload.GetConversationID()).
		Str("participant_id", payload.GetMessagePayload().GetParticipantID()).
		Bool("has_sim", payload.GetSIMPayload() != nil).
		Str("tmp_id", payload.GetTmpID())
	if conv != nil {
		ev = ev.Str("conv_type", conv.GetType().String()).
			Str("conv_status", conv.GetStatus().String()).
			Str("send_mode", conv.GetSendMode().String()).
			Bool("is_group", conv.GetIsGroupChat()).
			Bool("read_only", conv.GetReadOnly()).
			Int("participants", len(conv.GetParticipants())).
			Str("default_outgoing_id", conv.GetDefaultOutgoingID())
	}
	if age, ok := googleGroupAge(payload.GetConversationID()); ok {
		ev = ev.Int64("group_age_ms", age.Milliseconds())
	}
	if sendErr != nil {
		ev = ev.Err(sendErr)
	} else {
		ev = ev.Str("status", resp.GetStatus().String())
		if resp.GetGoogleAccountSwitch() != nil {
			ev = ev.Bool("account_switch", true)
		}
		// Fields Google sent that the proto doesn't know about may explain a
		// failure; they're small, so log them only when the send failed.
		if failed && resp != nil {
			if unknown := resp.ProtoReflect().GetUnknown(); len(unknown) > 0 {
				ev = ev.Hex("resp_unknown_fields", unknown)
			}
		}
	}
	ev.Msg("Google text send")
}
