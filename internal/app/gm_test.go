package app

import (
	"errors"
	"strings"
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"google.golang.org/protobuf/proto"
)

type groupCreateGM struct {
	GMClient
	answers  []*gmproto.GetOrCreateConversationResponse
	err      error
	errs     []error // per request, in order; nil entries answer normally
	requests []*gmproto.GetOrCreateConversationRequest
}

func (g *groupCreateGM) GetOrCreateConversation(req *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
	g.requests = append(g.requests, proto.Clone(req).(*gmproto.GetOrCreateConversationRequest))
	if g.err != nil {
		return nil, g.err
	}
	if len(g.errs) > 0 {
		err := g.errs[0]
		g.errs = g.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	answer := g.answers[0]
	g.answers = g.answers[1:]
	return answer, nil
}

func TestGetOrCreateGoogleGroupRetriesWhenGoogleAsksForRCS(t *testing.T) {
	gm := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS},
		{Conversation: &gmproto.Conversation{ConversationID: "rcs-group"}},
	}}

	conv, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, "Elders")
	if err != nil {
		t.Fatal(err)
	}
	if conv.GetConversationID() != "rcs-group" {
		t.Fatalf("conversation = %q", conv.GetConversationID())
	}
	if len(gm.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(gm.requests))
	}
	if gm.requests[0].CreateRCSGroup != nil {
		t.Fatal("first request set CreateRCSGroup before Google asked for it")
	}
	if !gm.requests[1].GetCreateRCSGroup() || gm.requests[1].GetRCSGroupName() != "Elders" {
		t.Fatalf("retry = create %v name %q", gm.requests[1].GetCreateRCSGroup(), gm.requests[1].GetRCSGroupName())
	}
}

func TestGetOrCreateGoogleGroupSendsAnEmptyNameOnlyOnTheRCSRetry(t *testing.T) {
	gm := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS},
		{Conversation: &gmproto.Conversation{ConversationID: "rcs-group"}},
	}}

	if _, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, ""); err != nil {
		t.Fatal(err)
	}
	if gm.requests[0].RCSGroupName != nil {
		t.Fatal("first request carried a group name nobody gave")
	}
	if gm.requests[1].RCSGroupName == nil {
		t.Fatal("RCS retry needs a name field, even an empty one")
	}
}

func TestGetOrCreateGoogleGroupKeepsAnMMSGroupToOneRequest(t *testing.T) {
	gm := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_SUCCESS, Conversation: &gmproto.Conversation{ConversationID: "mms-group"}},
	}}

	conv, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, "Elders")
	if err != nil {
		t.Fatal(err)
	}
	if conv.GetConversationID() != "mms-group" || len(gm.requests) != 1 {
		t.Fatalf("conversation %q after %d requests", conv.GetConversationID(), len(gm.requests))
	}
}

func TestGetOrCreateGoogleGroupReturnsTheError(t *testing.T) {
	want := errors.New("phone did not respond")
	gm := &groupCreateGM{err: want}

	if _, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, ""); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
	if len(gm.requests) != 1 {
		t.Fatalf("requests = %d, want 1: a failed first request is not an RCS failure", len(gm.requests))
	}
}

func assertMMSFallback(t *testing.T, req *gmproto.GetOrCreateConversationRequest) {
	t.Helper()
	if req.CreateRCSGroup == nil || req.GetCreateRCSGroup() {
		t.Fatalf("fallback CreateRCSGroup = %v, want explicit false", req.CreateRCSGroup)
	}
	if req.RCSGroupName != nil {
		t.Fatalf("fallback carried group name %q", req.GetRCSGroupName())
	}
	if len(req.GetNumbers()) != 2 {
		t.Fatalf("fallback numbers = %d", len(req.GetNumbers()))
	}
}

func TestGetOrCreateGoogleGroupFallsBackToMMSWhenTheRCSCreateIsEmpty(t *testing.T) {
	gm := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS},
		{Status: gmproto.GetOrCreateConversationResponse_UNKNOWN},
		{Conversation: &gmproto.Conversation{ConversationID: "mms-group"}},
	}}

	conv, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, "Elders")
	if err != nil {
		t.Fatal(err)
	}
	if conv.GetConversationID() != "mms-group" || len(gm.requests) != 3 {
		t.Fatalf("conversation %q after %d requests", conv.GetConversationID(), len(gm.requests))
	}
	assertMMSFallback(t, gm.requests[2])
}

func TestGetOrCreateGoogleGroupFallsBackToMMSWhenTheRCSCreateErrors(t *testing.T) {
	gm := &groupCreateGM{
		errs: []error{nil, errors.New("rcs refused")},
		answers: []*gmproto.GetOrCreateConversationResponse{
			{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS},
			{Conversation: &gmproto.Conversation{ConversationID: "mms-group"}},
		},
	}

	conv, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, "Elders")
	if err != nil {
		t.Fatal(err)
	}
	if conv.GetConversationID() != "mms-group" || len(gm.requests) != 3 {
		t.Fatalf("conversation %q after %d requests", conv.GetConversationID(), len(gm.requests))
	}
	assertMMSFallback(t, gm.requests[2])
}

func TestGetOrCreateGoogleGroupFallsBackToMMSWhenTheFirstAnswerIsEmpty(t *testing.T) {
	gm := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_UNKNOWN},
		{Conversation: &gmproto.Conversation{ConversationID: "mms-group"}},
	}}

	conv, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, "Elders")
	if err != nil {
		t.Fatal(err)
	}
	if conv.GetConversationID() != "mms-group" || len(gm.requests) != 2 {
		t.Fatalf("conversation %q after %d requests", conv.GetConversationID(), len(gm.requests))
	}
	assertMMSFallback(t, gm.requests[1])
}

func TestGetOrCreateGoogleGroupExplainsWhenTheMMSFallbackIsEmptyToo(t *testing.T) {
	gm := &groupCreateGM{answers: []*gmproto.GetOrCreateConversationResponse{
		{Status: gmproto.GetOrCreateConversationResponse_CREATE_RCS},
		{Status: gmproto.GetOrCreateConversationResponse_UNKNOWN},
		{Status: gmproto.GetOrCreateConversationResponse_UNKNOWN},
	}}

	_, err := GetOrCreateGoogleGroup(gm, []string{"+15551230001", "+15551230002"}, "Elders")
	if err == nil {
		t.Fatal("want an error when no attempt returns a thread")
	}
	for _, part := range []string{"RCS group create returned no conversation", "MMS fallback returned no conversation"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("err %q is missing %q", err, part)
		}
	}
	if len(gm.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(gm.requests))
	}
}
