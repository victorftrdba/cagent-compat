package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

const (
	titleSystemPrompt     = "You are a helpful AI assistant that generates concise, descriptive titles for conversations. You will be given a conversation history and asked to create a title that captures the main topic."
	titleUserPromptFormat = "Based on the following message a user sent to an AI assistant, generate a short, descriptive title (maximum 50 characters) that captures the main topic or purpose of the conversation. Return ONLY the title text, nothing else.\n\nUser message: %s\n\n"
)

type titleGenerator struct {
	model provider.Provider
}

func newTitleGenerator(model provider.Provider) *titleGenerator {
	return &titleGenerator{
		model: model,
	}
}

func (t *titleGenerator) Generate(ctx context.Context, sessionID, firstUserMessage string) <-chan string {
	result := make(chan string, 1)
	go func() {
		defer close(result)
		result <- t.generate(ctx, sessionID, firstUserMessage)
	}()
	return result
}

func (t *titleGenerator) generate(ctx context.Context, sessionID, firstUserMessage string) string {
	slog.Debug("Generating title for session", "session_id", sessionID)
	if firstUserMessage == "" {
		return ""
	}

	userPrompt := fmt.Sprintf(titleUserPromptFormat, firstUserMessage)

	titleModel := provider.CloneWithOptions(
		ctx,
		t.model,
		options.WithStructuredOutput(nil),
		options.WithMaxTokens(20),
		options.WithGeneratingTitle(),
	)

	newTeam := team.New(
		team.WithAgents(agent.New("root", titleSystemPrompt, agent.WithModel(titleModel))),
	)

	titleSession := session.New(
		session.WithUserMessage(userPrompt),
		session.WithTitle("Generating title..."),
	)

	titleRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create title generator runtime", "error", err)
		return ""
	}

	_, err = titleRuntime.Run(ctx, titleSession)
	if err != nil {
		slog.Error("Failed to generate session title", "session_id", sessionID, "error", err)
		return ""
	}

	title := titleSession.GetLastAssistantMessageContent()
	if title == "" {
		return ""
	}

	slog.Debug("Generated session title", "session_id", sessionID, "title", title)
	return title
}
