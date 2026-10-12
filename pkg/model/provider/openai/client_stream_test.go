package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
)

const fixtureStreamChunk = `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"fixture-model","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`

func TestClientStreamEmptyEvents(t *testing.T) {
	t.Parallel()

	data := "data: " + fixtureStreamChunk + "\n\n"
	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "comment", body: ": keepalive\n\n" + data, want: []string{"hello"}},
		{name: "retry", body: "retry: 1000\n\n" + data, want: []string{"hello"}},
		{name: "CRLF", body: ": keepalive\r\n\r\nretry: 1000\r\n\r\n\r\n" + strings.ReplaceAll(data, "\n", "\r\n"), want: []string{"hello"}},
		{name: "multiple empty blocks", body: "\n\n: first\n\nretry: 1000\n\n\n" + data + ": between\n\n\n" + data + ": last\n\n", want: []string{"hello", "hello"}},
		{name: "empty events then EOF", body: ": keepalive\n\nretry: 1000\n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stream := newFixtureClientStream(t, tt.body, http.StatusOK, false)
			var got []string
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				require.Len(t, chunk.Choices, 1)
				assert.Equal(t, "fixture", chunk.ID)
				assert.Equal(t, "fixture-model", chunk.Model)
				assert.Equal(t, "assistant", chunk.Choices[0].Delta.Role)
				got = append(got, chunk.Choices[0].Delta.Content)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientStreamErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{name: "explicit empty data", body: ": keepalive\n\ndata:\n\n", status: http.StatusOK, want: "unexpected end of JSON input"},
		{name: "stream API error", body: ": keepalive\n\ndata: {" + `"error":{"message":"fixture failure","type":"invalid_request_error"}` + "}\n\n", status: http.StatusOK, want: "fixture failure"},
		{name: "HTTP API error", body: `{"error":{"message":"fixture failure","type":"invalid_request_error"}}`, status: http.StatusBadRequest, want: "fixture failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stream := newFixtureClientStream(t, tt.body, tt.status, false)
			_, err := stream.Recv()
			require.ErrorContains(t, err, tt.want)
			assert.NotErrorIs(t, err, io.EOF)
		})
	}
}

func TestClientStreamReaderError(t *testing.T) {
	t.Parallel()

	stream := newFixtureClientStream(t, "data: "+fixtureStreamChunk+"\n\n: keepalive\n\n", http.StatusOK, true)
	chunk, err := stream.Recv()
	require.NoError(t, err)
	require.Len(t, chunk.Choices, 1)
	assert.Equal(t, "hello", chunk.Choices[0].Delta.Content)
	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func newFixtureClientStream(t *testing.T, body string, status int, truncated bool) chat.MessageStream {
	t.Helper()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer offline-test-key", r.Header.Get("Authorization"))
		var request struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "fixture-model", request.Model)
		assert.True(t, request.Stream)
		w.Header().Set("Content-Type", "text/event-stream")
		if truncated {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
		}
		w.WriteHeader(status)
		_, err := io.WriteString(w, body)
		assert.NoError(t, err)
	}))
	t.Cleanup(func() {
		server.Close()
		assert.Equal(t, int32(1), requests.Load())
	})

	trackUsage := false
	client, err := NewClient(t.Context(), &latest.ModelConfig{
		Provider:   "openai",
		Model:      "fixture-model",
		BaseURL:    server.URL + "/v1",
		TrackUsage: &trackUsage,
	}, environment.NewEnvListProvider([]string{"OPENAI_API_KEY=offline-test-key"}))
	require.NoError(t, err)
	stream, err := client.CreateChatCompletionStream(t.Context(), []chat.Message{{Role: chat.MessageRoleUser, Content: "fixture"}}, nil)
	require.NoError(t, err)
	t.Cleanup(stream.Close)
	return stream
}
