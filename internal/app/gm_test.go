package app

import (
	"errors"
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"google.golang.org/protobuf/proto"
)

type groupCreateGM struct {
	GMClient
	answers  []*gmproto.GetOrCreateConversationResponse
	err      error
	requests []*gmproto.GetOrCreateConversationRequest
}

func (g *groupCreateGM) GetOrCreateConversation(req *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
	g.requests = append(g.requests, proto.Clone(req).(*gmproto.GetOrCreateConversationRequest))
	if g.err != nil {
		return nil, g.err
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
}
