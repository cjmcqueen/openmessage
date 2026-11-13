package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func withGroupClock(t *testing.T, now time.Time) *time.Time {
	t.Helper()
	clock := now
	googleGroupsCreated.Lock()
	savedAt, savedNow := googleGroupsCreated.at, googleGroupsCreated.now
	googleGroupsCreated.at = nil
	googleGroupsCreated.now = func() time.Time { return clock }
	googleGroupsCreated.Unlock()
	t.Cleanup(func() {
		googleGroupsCreated.Lock()
		googleGroupsCreated.at, googleGroupsCreated.now = savedAt, savedNow
		googleGroupsCreated.Unlock()
	})
	return &clock
}

func logSend(t *testing.T, conv *gmproto.Conversation, payload *gmproto.SendMessageRequest, resp *gmproto.SendMessageResponse, err error) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	LogGoogleTextSend(zerolog.New(&buf), "test", conv, payload, resp, err)
	var line map[string]any
	if jsonErr := json.Unmarshal(buf.Bytes(), &line); jsonErr != nil {
		t.Fatalf("log line %q: %v", buf.String(), jsonErr)
	}
	return line
}

func TestLogGoogleTextSendRecordsAFailedGroupSendWithoutTheBody(t *testing.T) {
	clock := withGroupClock(t, time.Unix(1_800_000_000, 0))
	NoteGoogleGroupCreated("rcs-group")
	*clock = clock.Add(1500 * time.Millisecond)

	conv := &gmproto.Conversation{
		ConversationID:    "rcs-group",
		IsGroupChat:       true,
		Type:              gmproto.ConversationType_RCS,
		DefaultOutgoingID: "outgoing-7",
		Participants:      []*gmproto.Participant{{}, {}, {}, {}},
	}
	payload := BuildSendPayload("rcs-group", "secret reminder text", "", "+15551230000", &gmproto.SIMPayload{})
	line := logSend(t, conv, payload, &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_FAILURE_3}, nil)

	want := map[string]any{
		"level":               "warn",
		"source":              "test",
		"conv_id":             "rcs-group",
		"participant_id":      "+15551230000",
		"has_sim":             true,
		"conv_type":           "RCS",
		"is_group":            true,
		"participants":        float64(4),
		"default_outgoing_id": "outgoing-7",
		"group_age_ms":        float64(1500),
		"status":              "FAILURE_3",
	}
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %v, want %v", key, line[key], value)
		}
	}
	for key, value := range line {
		if s, ok := value.(string); ok && strings.Contains(s, "secret") {
			t.Errorf("log field %s leaked the message body: %q", key, s)
		}
	}
}

func TestLogGoogleTextSendLogsSuccessAtInfoWithoutAGroupAge(t *testing.T) {
	withGroupClock(t, time.Unix(1_800_000_000, 0))
	payload := BuildSendPayload("one-to-one", "hi", "", "+15551230000", nil)
	line := logSend(t, nil, payload, &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil)

	if line["level"] != "info" || line["status"] != "SUCCESS" || line["has_sim"] != false {
		t.Fatalf("line = %v", line)
	}
	if _, ok := line["group_age_ms"]; ok {
		t.Fatalf("group_age_ms logged for a conversation this process didn't create: %v", line)
	}
}

func TestLogGoogleTextSendLogsTheSendError(t *testing.T) {
	payload := BuildSendPayload("conv", "hi", "", "", nil)
	line := logSend(t, nil, payload, nil, errors.New("connection reset"))

	if line["level"] != "warn" || line["error"] != "connection reset" {
		t.Fatalf("line = %v", line)
	}
	if _, ok := line["status"]; ok {
		t.Fatalf("status logged alongside an error: %v", line)
	}
}

func TestGoogleGroupAgeForgetsOldGroups(t *testing.T) {
	clock := withGroupClock(t, time.Unix(1_800_000_000, 0))
	NoteGoogleGroupCreated("old")
	*clock = clock.Add(googleGroupCreatedTTL + time.Second)
	NoteGoogleGroupCreated("new")

	if _, ok := googleGroupAge("old"); ok {
		t.Fatal("old group still has an age")
	}
	googleGroupsCreated.Lock()
	_, kept := googleGroupsCreated.at["old"]
	googleGroupsCreated.Unlock()
	if kept {
		t.Fatal("old group was not pruned")
	}
	if age, ok := googleGroupAge("new"); !ok || age != 0 {
		t.Fatalf("new group age = %v, %v", age, ok)
	}
}

func TestGetOrCreateGoogleGroupNotesANewRCSGroupOnly(t *testing.T) {
	withGroupClock(t, time.Unix(1_800_000_000, 0))
	rcs := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS},
		{Conversation: &gmproto.Conversation{ConversationID: "rcs-group"}},
	}}
	if _, err := GetOrCreateGoogleGroup(rcs, []string{"+15551230001", "+15551230002"}, "Elders"); err != nil {
		t.Fatal(err)
	}
	existing := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Conversation: &gmproto.Conversation{ConversationID: "existing-group"}},
	}}
	if _, err := GetOrCreateGoogleGroup(existing, []string{"+15551230001", "+15551230003"}, ""); err != nil {
		t.Fatal(err)
	}

	if _, ok := googleGroupAge("rcs-group"); !ok {
		t.Fatal("new RCS group was not noted")
	}
	if _, ok := googleGroupAge("existing-group"); ok {
		t.Fatal("a group Google returned directly was noted as new")
	}
}
