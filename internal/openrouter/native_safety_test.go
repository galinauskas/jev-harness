package openrouter

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNativeCompletionAndReportedCost(t *testing.T) {
	for _, protocol := range []string{"messages", "responses"} {
		for _, cost := range []string{"0", "null"} {
			t.Run(protocol+"/cost="+cost, func(t *testing.T) {
				var payload string
				if protocol == "messages" {
					payload = fmt.Sprintf("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call-1\",\"name\":\"read_file\",\"input\":{\"path\":\"a.txt\"}}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":3,\"cost\":%s}}\n\ndata: {\"type\":\"message_stop\"}\n\n", cost)
				} else {
					payload = fmt.Sprintf(`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","call_id":"call-1","name":"read_file","arguments":"{\"path\":\"a.txt\"}"}],"usage":{"input_tokens":2,"output_tokens":3,"cost":%s}}}`+"\n\n", cost)
				}
				for _, interrupted := range []bool{false, true} {
					body := payload
					if interrupted {
						body = body[:len(body)/2]
					}
					ch := make(chan StreamEvent, 16)
					readGoStream(context.Background(), io.NopCloser(strings.NewReader(body)), time.Now(), protocol, "opencode-go", ch)
					var final StreamEvent
					for event := range ch {
						if event.Done {
							final = event
						}
					}
					if interrupted {
						if final.Err == nil || len(final.ToolCalls) != 0 {
							t.Fatal("partial stream delivered executable calls", final)
						}
						continue
					}
					if final.Err != nil || len(final.ToolCalls) != 1 || len(final.NativeItems) != 1 {
						t.Fatal("valid native call lost", final)
					}
					if final.Usage == nil || final.Usage.TotalTokens != 5 || final.Usage.CostKnown != (cost == "0") {
						t.Fatal("native usage lost cost availability", final.Usage)
					}
				}
			})
		}
	}
}
