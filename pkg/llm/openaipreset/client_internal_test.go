package openaipreset

import (
	"net/http"
	"testing"
)

func TestDefaultHTTPClientAllowsSlowFirstHeader(t *testing.T) {
	client := defaultHTTPClient()
	if client.Timeout != defaultRequestTimeout {
		t.Fatalf("Timeout = %v, want %v", client.Timeout, defaultRequestTimeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}

	if transport.ResponseHeaderTimeout != defaultRequestTimeout {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, defaultRequestTimeout)
	}
}

func TestPromptCacheKeyFor(t *testing.T) {
	t.Parallel()

	withPrefix := &Client{promptCacheKeyPrefix: "twentyq:"}
	if got := withPrefix.promptCacheKeyFor(" answer_question "); got != "twentyq:answer_question" {
		t.Fatalf("promptCacheKeyFor() = %q, want twentyq:answer_question", got)
	}

	noPrefix := &Client{}
	if got := noPrefix.promptCacheKeyFor("answer_question"); got != "" {
		t.Fatalf("promptCacheKeyFor() without prefix = %q, want empty", got)
	}
}

func TestCacheBreakpointMessageAssistantRendersPlain(t *testing.T) {
	t.Parallel()

	item := cacheBreakpointMessage("이전 답변", "assistant")
	if item.OfMessage == nil {
		t.Fatal("assistant breakpoint item is not a message param")
	}

	if item.OfMessage.Content.OfInputItemContentList != nil {
		t.Fatalf("assistant breakpoint rendered a content list: %#v", item.OfMessage.Content)
	}

	if got := item.OfMessage.Content.OfString.Value; got != "이전 답변" {
		t.Fatalf("assistant content = %q, want plain string content", got)
	}

	userItem := cacheBreakpointMessage("질문", "user")
	if userItem.OfMessage == nil || userItem.OfMessage.Content.OfInputItemContentList == nil {
		t.Fatalf("user breakpoint must keep the input_text block: %#v", userItem)
	}
}
