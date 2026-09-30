package openrouter

// Snapshot of https://models.dev/api.json, opencode-go catalog (2026-09-30).
// Unknown models can still use chat completions, but need catalog updates for
// automatic compaction. Protocol overrides follow OpenCode Go's endpoint docs.
type goModelInfo struct {
	Context  int
	Protocol string
}

var goModels = map[string]goModelInfo{
	"deepseek-v4-flash":            {1000000, "chat"},
	"deepseek-v4-flash-vision-exp": {1000000, "chat"},
	"deepseek-v4-pro":              {1000000, "chat"},
	"deepseek-v4.1-flash":          {1000000, "chat"},
	"glm-5.2":                      {1000000, "chat"},
	"glm-5.3":                      {1000000, "chat"},
	"glm-5.3-flash":                {1000000, "chat"},
	"gpt-5.6-luna":                 {922000, "responses"},
	"gpt-6-luna":                   {922000, "responses"},
	"grok-4.5":                     {500000, "responses"},
	"grok-4.6":                     {500000, "responses"},
	"grok-4.7":                     {500000, "responses"},
	"hy3":                          {192000, "chat"},
	"hy4-preview":                  {1024000, "chat"},
	"kimi-k2.6":                    {262144, "chat"},
	"kimi-k2.7-code":               {262144, "chat"},
	"kimi-k3":                      {1048576, "chat"},
	"longcat-2.0":                  {1000000, "chat"},
	"longcat-2.5-preview-free":     {1000000, "chat"},
	"mimo-v2.5":                    {1000000, "chat"},
	"mimo-v2.5-pro":                {1048576, "chat"},
	"mimo-v2.6-flash":              {1048576, "chat"},
	"mimo-v2.6-pro":                {1048576, "chat"},
	"minimax-m2.7":                 {204800, "messages"},
	"minimax-m3":                   {1000000, "messages"},
	"muse-spark-1.2-contributor":   {1048576, "responses"},
	"muse-spark-1.3-contributor":   {1048576, "responses"},
	"qwen3.6-plus":                 {1000000, "messages"},
	"qwen3.7-max":                  {1000000, "messages"},
	"qwen3.7-plus":                 {1000000, "messages"},
	"qwen3.8-flash":                {1000000, "messages"},
	"qwen3.8-max":                  {1000000, "messages"},
	"space-bunny-free":             {524288, "chat"},
}
