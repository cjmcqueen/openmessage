package app

import (
	"fmt"
	"math/rand"
	"strings"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var (
	getGoogleConversationForSend = func(a *App, conversationID string) (*gmproto.Conversation, error) {
		cli := a.GetClient()
		if cli == nil {
			return nil, fmt.Errorf(ErrNotConnected)
		}
		return cli.GM.GetConversation(conversationID)
	}
	sendGoogleTextPayload = func(a *App, payload *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		cli := a.GetClient()
		if cli == nil {
			return nil, fmt.Errorf(ErrNotConnected)
		}
		return cli.GM.SendMessage(payload)
	}
	uploadGoogleMedia = func(a *App, data []byte, filename, mime string) (*gmproto.MediaContent, error) {
		cli := a.GetClient()
		if cli == nil {
			return nil, fmt.Errorf(ErrNotConnected)
		}
		return cli.GM.UploadMedia(data, filename, mime)
	}
)

// ErrNotConnected is the error message returned when an operation requires
// a Google Messages connection but one is not established.
const ErrNotConnected = "not connected to Google Messages"

// ContactNumberMysteriousInt is the default value for the MysteriousInt field
// in ContactNumber structs used for conversation lookups and message sending.
const ContactNumberMysteriousInt = 7

// NewContactNumbers builds a ContactNumber slice from phone number strings,
// suitable for GetOrCreateConversation requests.
func NewContactNumbers(phones []string) []*gmproto.ContactNumber {
	numbers := make([]*gmproto.ContactNumber, len(phones))
	for i, phone := range phones {
		numbers[i] = &gmproto.ContactNumber{
			MysteriousInt: ContactNumberMysteriousInt,
			Number:        phone,
			Number2:       phone,
		}
	}
	return numbers
}

// GetOrCreateGoogleGroup finds or creates the group thread for phones. When
// every member can use RCS, Google answers CREATE_RCS with no conversation and
// expects the request again with CreateRCSGroup set; otherwise the first answer
// is the thread itself, an MMS group if any member lacks RCS. name titles a new
// RCS group and may be empty.
//
// Google sometimes answers without a thread: the RCS create fails, or the first
// answer has no conversation. Then it asks once more as a plain MMS group, with
// RCS turned off and no name, since MMS groups have no title. An error from the
// first request is returned as is: the phone or session failed, not RCS.
func GetOrCreateGoogleGroup(gm GMClient, phones []string, name string) (*gmproto.Conversation, error) {
	request := &gmproto.GetOrCreateConversationRequest{Numbers: NewContactNumbers(phones)}
	if name != "" {
		request.RCSGroupName = &name
	}
	response, err := gm.GetOrCreateConversation(request)
	if err != nil {
		return nil, err
	}
	failure := fmt.Sprintf("no conversation in Google's answer (status %s)", response.GetStatus())
	if response.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		createRCSGroup := true
		request.RCSGroupName = &name
		request.CreateRCSGroup = &createRCSGroup
		response, err = gm.GetOrCreateConversation(request)
		if err == nil && response.GetConversation() != nil {
			NoteGoogleGroupCreated(response.GetConversation().GetConversationID())
			return response.GetConversation(), nil
		}
		if err != nil {
			failure = fmt.Sprintf("RCS group create failed: %v", err)
		} else {
			failure = fmt.Sprintf("RCS group create returned no conversation (status %s)", response.GetStatus())
		}
	} else if response.GetConversation() != nil {
		return response.GetConversation(), nil
	}

	createRCSGroup := false
	mms := &gmproto.GetOrCreateConversationRequest{
		Numbers:        NewContactNumbers(phones),
		CreateRCSGroup: &createRCSGroup,
	}
	response, err = gm.GetOrCreateConversation(mms)
	if err != nil {
		return nil, fmt.Errorf("%s; MMS fallback failed: %w", failure, err)
	}
	if response.GetConversation() == nil {
		return nil, fmt.Errorf("%s; MMS fallback returned no conversation (status %s)", failure, response.GetStatus())
	}
	return response.GetConversation(), nil
}

// ExtractSIMAndParticipant finds the current user's participant ID and SIM
// payload from a conversation, falling back to the conversation's SIM card.
//
// The sender is the conversation's DefaultOutgoingID, Google's participant ID
// for this account (such as "2"), as mautrix-gmessages sends it. A brand-new
// RCS group answers FAILURE_3 to a send that names the sender by phone number
// instead, so the self participant's number is only a fallback for a
// conversation that carries no DefaultOutgoingID.
func ExtractSIMAndParticipant(conv *gmproto.Conversation) (participantID string, sim *gmproto.SIMPayload) {
	for _, p := range conv.GetParticipants() {
		if p.GetIsMe() {
			if id := p.GetID(); id != nil {
				participantID = id.GetNumber()
			}
			sim = p.GetSimPayload()
			break
		}
	}
	if outgoing := conv.GetDefaultOutgoingID(); outgoing != "" {
		participantID = outgoing
	}
	if sim == nil {
		if sc := conv.GetSimCard(); sc != nil {
			sim = sc.GetSIMData().GetSIMPayload()
		}
	}
	return
}

// BuildSendPayload constructs a SendMessageRequest matching the format used by
// the mautrix bridge: MessageInfo array (not MessagePayloadContent), TmpID in 3
// places, SIMPayload, and ParticipantID.
func BuildSendPayload(conversationID, message, replyToID, participantID string, sim *gmproto.SIMPayload) *gmproto.SendMessageRequest {
	return BuildSendPayloadWithTmpID(conversationID, message, replyToID, participantID, sim, "")
}

func newSendTmpID(preferred string) string {
	if preferred = strings.TrimSpace(preferred); preferred != "" {
		return preferred
	}
	return fmt.Sprintf("tmp_%012d", rand.Int63n(1e12))
}

// BuildSendPayloadWithTmpID is BuildSendPayload with an optional caller-owned
// temporary ID. Queued sends use this to make retries idempotent server-side.
func BuildSendPayloadWithTmpID(conversationID, message, replyToID, participantID string, sim *gmproto.SIMPayload, tmpID string) *gmproto.SendMessageRequest {
	tmpID = newSendTmpID(tmpID)
	req := &gmproto.SendMessageRequest{
		ConversationID: conversationID,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:                 tmpID,
			MessagePayloadContent: nil,
			MessageInfo: []*gmproto.MessageInfo{{
				Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{
					Content: message,
				}},
			}},
			ConversationID: conversationID,
			ParticipantID:  participantID,
			TmpID2:         tmpID,
		},
		SIMPayload: sim,
		TmpID:      tmpID,
	}
	if replyToID != "" {
		req.Reply = &gmproto.ReplyPayload{
			MessageID: replyToID,
		}
	}
	return req
}

// BuildSendMediaPayload constructs a SendMessageRequest with a MediaContent attachment
// instead of text. Uses the same MessageInfo array format as BuildSendPayload.
func BuildSendMediaPayload(conversationID string, media *gmproto.MediaContent, participantID string, sim *gmproto.SIMPayload) *gmproto.SendMessageRequest {
	return BuildSendMediaPayloadWithTmpID(conversationID, media, participantID, sim, "")
}

// BuildSendMediaPayloadWithTmpID is BuildSendMediaPayload with an optional
// caller-owned temporary ID for idempotent queued media retries.
func BuildSendMediaPayloadWithTmpID(conversationID string, media *gmproto.MediaContent, participantID string, sim *gmproto.SIMPayload, tmpID string) *gmproto.SendMessageRequest {
	tmpID = newSendTmpID(tmpID)
	return &gmproto.SendMessageRequest{
		ConversationID: conversationID,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:                 tmpID,
			MessagePayloadContent: nil,
			MessageInfo: []*gmproto.MessageInfo{{
				Data: &gmproto.MessageInfo_MediaContent{MediaContent: media},
			}},
			ConversationID: conversationID,
			ParticipantID:  participantID,
			TmpID2:         tmpID,
		},
		SIMPayload: sim,
		TmpID:      tmpID,
	}
}

// BuildReactionPayload constructs a SendReactionRequest using
// gmproto.MakeReactionData for proper emoji type mapping.
func BuildReactionPayload(messageID, emoji, action string, sim *gmproto.SIMPayload) *gmproto.SendReactionRequest {
	var a gmproto.SendReactionRequest_Action
	switch strings.ToLower(action) {
	case "remove":
		a = gmproto.SendReactionRequest_REMOVE
	case "switch":
		a = gmproto.SendReactionRequest_SWITCH
	default:
		a = gmproto.SendReactionRequest_ADD
	}
	return &gmproto.SendReactionRequest{
		MessageID:    messageID,
		ReactionData: gmproto.MakeReactionData(emoji),
		Action:       a,
		SIMPayload:   sim,
	}
}
