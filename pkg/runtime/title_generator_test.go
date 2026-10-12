package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
)

type blockingTitleProvider struct {
	started chan string
	release chan struct{}
}

func (p *blockingTitleProvider) ID() string              { return "test/title" }
func (p *blockingTitleProvider) BaseConfig() base.Config { return base.Config{} }
func (p *blockingTitleProvider) CreateChatCompletionStream(ctx context.Context, messages []chat.Message, _ []tools.Tool) (chat.MessageStream, error) {
	for _, message := range messages {
		if strings.HasPrefix(message.Content, "Based on the following message a user sent") {
			p.started <- message.Content
			select {
			case <-p.release:
				return newStreamBuilder().AddContent("Generated title").AddStopWithUsage(1, 1).Build(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return newStreamBuilder().AddContent("Assistant answer").AddStopWithUsage(1, 1).Build(), nil
}

type titleSnapshotStore struct{ titles []string }

func (s *titleSnapshotStore) UpdateSession(_ context.Context, sess *session.Session) error {
	s.titles = append(s.titles, sess.Title)
	return nil
}

func TestTitleGeneratorPreservesCallerSession(t *testing.T) {
	model := &blockingTitleProvider{started: make(chan string, 1), release: make(chan struct{})}
	sess := session.New(session.WithUserMessage("Original request"))
	generator := newTitleGenerator(model)
	result := generator.Generate(t.Context(), sess.ID, sess.GetLastUserMessageContent())
	prompt := <-model.started
	sess.AddMessage(session.UserMessage("Later request"))
	sess.Title = "Caller-owned title"
	close(model.release)
	require.Equal(t, "Generated title", <-result)
	_, open := <-result
	require.False(t, open)
	require.Contains(t, prompt, "Original request")
	require.NotContains(t, prompt, "Later request")
	require.Equal(t, "Caller-owned title", sess.Title)
}

func TestRunStreamPersistsGeneratedTitle(t *testing.T) {
	model := &blockingTitleProvider{started: make(chan string, 1), release: make(chan struct{})}
	store := &titleSnapshotStore{}
	rt, err := New(team.New(team.WithAgents(agent.New("root", "Test", agent.WithModel(model)))), WithSessionCompaction(false), WithModelStore(mockModelStore{}), WithSessionStore(store))
	require.NoError(t, err)
	sess := session.New(session.WithUserMessage("Original request"))
	stream := rt.RunStream(t.Context(), sess)
	mutated := make(chan struct{})
	stopped := make(chan struct{}, 1)
	drained := make(chan []Event, 1)
	go func() {
		var events []Event
		for event := range stream {
			events = append(events, event)
			if _, ok := event.(*TokenUsageEvent); ok {
				close(mutated)
			}
			if _, ok := event.(*StreamStoppedEvent); ok {
				stopped <- struct{}{}
			}
		}
		drained <- events
	}()
	<-model.started
	<-mutated
	select {
	case <-stopped:
		t.Fatal("stream stopped before title settled")
	default:
	}
	close(model.release)
	events := <-drained
	require.Equal(t, "Generated title", sess.Title)
	require.NotEmpty(t, store.titles)
	require.Equal(t, "Generated title", store.titles[len(store.titles)-1])
	require.IsType(t, &StreamStoppedEvent{}, events[len(events)-1])
	var titleEvents int
	for _, event := range events {
		if title, ok := event.(*SessionTitleEvent); ok {
			titleEvents++
			require.Equal(t, sess.ID, title.SessionID)
			require.Equal(t, sess.Title, title.Title)
		}
	}
	require.Equal(t, 1, titleEvents)
}

func TestTitleGeneratorSettlesCancellation(t *testing.T) {
	model := &blockingTitleProvider{started: make(chan string, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := newTitleGenerator(model).Generate(ctx, "test-session", "Original request")
	<-model.started
	cancel()
	title, open := <-result
	require.True(t, open)
	require.Empty(t, title)
	_, open = <-result
	require.False(t, open)
}

func TestTitleGeneratorSettlesEmptyAndFailure(t *testing.T) {
	for name, test := range map[string]struct {
		model   *mockProviderWithError
		message string
	}{
		"empty": {nil, ""}, "provider-error": {&mockProviderWithError{id: "test/error"}, "Original request"},
	} {
		t.Run(name, func(t *testing.T) {
			result := newTitleGenerator(test.model).Generate(t.Context(), "test-session", test.message)
			title, open := <-result
			require.True(t, open)
			require.Empty(t, title)
			_, open = <-result
			require.False(t, open)
		})
	}
}
