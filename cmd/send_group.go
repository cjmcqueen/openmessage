package cmd

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/app"
)

func RunSendGroup(logger zerolog.Logger, phones []string, message string) error {
	return runSendGroupWithDeps(defaultSendGroupCommandDeps(func(phones []string, message string) error {
		return runLegacySendGroup(logger, phones, message)
	}), phones, message)
}

func runLegacySendGroup(logger zerolog.Logger, phones []string, message string) error {
	a, err := app.New(logger)
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer a.Close()

	if err := a.LoadAndConnect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	cli := a.GetClient()
	if cli == nil {
		return fmt.Errorf("client not connected")
	}

	conv, err := app.GetOrCreateGoogleGroup(cli.GM, phones, "")
	if err != nil {
		a.HandleGoogleAuthExpiredError(err)
		return fmt.Errorf("get/create group conversation: %w", err)
	}

	if conv == nil {
		return fmt.Errorf("no conversation returned")
	}

	myParticipantID, simPayload := app.ExtractSIMAndParticipant(conv)
	payload := app.BuildSendPayload(conv.GetConversationID(), message, "", myParticipantID, simPayload)
	_, err = cli.GM.SendMessage(payload)
	if err != nil {
		a.HandleGoogleAuthExpiredError(err)
		return fmt.Errorf("send: %w", err)
	}

	logger.Info().
		Str("conversation", conv.GetConversationID()).
		Str("recipients", strings.Join(phones, ", ")).
		Msg("Group message sent")
	return nil
}
