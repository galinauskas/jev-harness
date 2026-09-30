package router

import (
	"context"
	"jevharness/internal/config"
	"jevharness/internal/openrouter"
	"testing"
)

func TestProviderPolicyBeforeClassification(t *testing.T) {
	cfg := config.Default()
	cfg.Roles = append(cfg.Roles, config.Role{Name: "direct", Provider: "deepseek", Model: "deepseek-flash", Description: "direct"})
	cfg.Safety.Projects = map[string][]string{"/project": {"deepseek"}}
	r := New(openrouter.New(""), cfg)
	d := r.RouteProject(context.Background(), "/project", State{Message: "sensitive"})
	if d.Role.Backend() != "deepseek" || d.Source != SourceSingle {
		t.Fatal("forbidden classifier/fallback used", d)
	}
	cfg.Safety.Projects["/project"] = []string{}
	d = New(openrouter.New(""), cfg).RouteProject(context.Background(), "/project", State{})
	if d.Role.Name != "" {
		t.Fatal("empty policy failed open")
	}
	cfg.Roles = append(cfg.Roles, config.Role{Name: "other", Provider: "deepseek", Model: "other", Description: "other"})
	cfg.Safety.Projects["/project"] = []string{"deepseek"}
	d = New(openrouter.New(""), cfg).RouteProject(context.Background(), "/project", State{})
	if d.Err == nil {
		t.Fatal("classified through forbidden OpenRouter")
	}
}
