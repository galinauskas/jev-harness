// Package router maps each user turn to a model role by asking Jev
// (a Decisions API choice question whose criteria are the configured roles).
package router

import (
	"context"
	"errors"
	"fmt"
	"math"

	"jevharness/internal/config"
	"jevharness/internal/openrouter"
)

// Source explains how a Decision was produced.
type Source string

const (
	SourceJev       Source = "jev"       // confident choice
	SourceThreshold Source = "threshold" // below threshold -> default role
	SourceError     Source = "jev-error" // Decide failed -> default role
	SourceSingle    Source = "single"    // only one role configured
	SourcePinned    Source = "pinned"    // /role <name>
)

// Decision is the routing result for one user turn.
type Decision struct {
	Role          config.Role
	Source        Source
	Confidence    float64            // 0 when not from Jev
	Probabilities map[string]float64 // nil when not from Jev
	Usage         *openrouter.Usage  // reported routing request usage
	Model         string             // routing model
	Err           error              // set with SourceError
}

// State is what Jev sees when classifying a turn.
type State struct {
	Message     string `json:"message"`                // the new user message, full
	RecentTurns []Turn `json:"recent_turns,omitempty"` // last 6 user/assistant text messages, oldest first
}

// Turn is one truncated transcript line in State.
type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Router resolves user turns to roles.
type Router struct {
	client *openrouter.Client
	cfg    config.Config
}

// New returns a Router bound to cfg (the live config at call time, rebuilt
// via SetConfig after a settings save).
func New(client *openrouter.Client, cfg config.Config) *Router {
	return &Router{client: client, cfg: cfg}
}

// Question builds the Jev Choice question from the current roles. The
// criteria map is derived from config at call time, so a saved settings
// change is live on the next turn.
func (r *Router) Question() openrouter.ChoiceQuestion {
	crit := make(map[string]string, len(r.cfg.Roles))
	for _, role := range r.cfg.Roles {
		crit[role.Name] = role.Description
	}
	return openrouter.ChoiceQuestion{
		Type: "choice",
		Instructions: "Which model role should handle the user's latest message? " +
			"Judge the difficulty and scope of the work the message asks for, " +
			"using recent_turns only as context.",
		Criteria: crit,
	}
}

// Route picks a role for the turn described by state.
func (r *Router) Route(ctx context.Context, state State) Decision {
	fallback := func(src Source, conf float64, probs map[string]float64, err error) Decision {
		role, ok := r.cfg.RoleByName(r.cfg.DefaultRole)
		if !ok && len(r.cfg.Roles) > 0 {
			role = r.cfg.Roles[0]
		}
		return Decision{Role: role, Source: src, Confidence: conf, Probabilities: probs, Err: err}
	}

	if len(r.cfg.Roles) == 0 {
		return Decision{Source: SourceError, Err: errors.New("no roles configured")}
	}
	if len(r.cfg.Roles) == 1 {
		return Decision{Role: r.cfg.Roles[0], Source: SourceSingle}
	}

	resp, err := r.client.Decide(ctx, openrouter.DecisionsRequest{
		Model:     r.cfg.JevModel,
		State:     state,
		Questions: map[string]openrouter.ChoiceQuestion{"role": r.Question()},
	})
	if err != nil {
		return fallback(SourceError, 0, nil, err)
	}
	usage := &openrouter.Usage{PromptTokens: resp.Usage.InputTokens, TotalTokens: resp.Usage.InputTokens, CostKnown: resp.Usage.Cost != nil}
	if resp.Usage.Cost != nil {
		usage.Cost = *resp.Usage.Cost
	}
	withUsage := func(d Decision) Decision {
		d.Usage, d.Model = usage, resp.Model
		if d.Model == "" {
			d.Model = r.cfg.JevModel
		}
		return d
	}
	ans, ok := resp.Answers["role"]
	if !ok {
		return withUsage(fallback(SourceError, 0, nil, errors.New("jev returned no answer for question \"role\"")))
	}
	role, ok := r.cfg.RoleByName(ans.Choice)
	if !ok {
		return withUsage(fallback(SourceError, 0, ans.Probabilities,
			fmt.Errorf("jev chose unknown role %q", ans.Choice)))
	}
	conf := 0.0
	hasConfidence := false
	if ans.Confidence != nil {
		conf = *ans.Confidence
		hasConfidence = true
	} else if p, ok := ans.Probabilities[ans.Choice]; ok {
		conf = p
		hasConfidence = true
	}
	if hasConfidence && (math.IsNaN(conf) || math.IsInf(conf, 0) || conf < 0 || conf > 1) {
		return withUsage(fallback(SourceError, 0, ans.Probabilities, fmt.Errorf("jev returned invalid confidence %g", conf)))
	}
	if (!hasConfidence && r.cfg.ConfidenceThreshold > 0) || conf < r.cfg.ConfidenceThreshold {
		return withUsage(fallback(SourceThreshold, conf, ans.Probabilities, nil))
	}
	return withUsage(Decision{Role: role, Source: SourceJev, Confidence: conf, Probabilities: ans.Probabilities})
}
