package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config JSON: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string
	if len(c.Providers) == 0 {
		problems = append(problems, "config.providers is empty")
	}
	for name, provider := range c.Providers {
		if strings.TrimSpace(name) == "" {
			problems = append(problems, "provider name must not be empty")
		}
		base := strings.TrimSpace(provider.BaseURL)
		if base == "" {
			problems = append(problems, fmt.Sprintf("provider %q base_url is empty", name))
		} else if u, err := url.Parse(base); err != nil || u.Scheme == "" || u.Host == "" {
			problems = append(problems, fmt.Sprintf("provider %q base_url is invalid", name))
		}
		if !provider.supports(APIChat) && !provider.supports(APIResponses) {
			problems = append(problems, fmt.Sprintf("provider %q supports must include chat or responses", name))
		}
	}
	for name, target := range c.Targets {
		if strings.TrimSpace(name) == "" {
			problems = append(problems, "target name must not be empty")
		}
		if strings.TrimSpace(target.Provider) == "" {
			problems = append(problems, fmt.Sprintf("target %q provider is empty", name))
		} else if _, ok := c.Providers[target.Provider]; !ok {
			problems = append(problems, fmt.Sprintf("target %q references missing provider %q", name, target.Provider))
		}
		if strings.TrimSpace(target.Model) == "" {
			problems = append(problems, fmt.Sprintf("target %q model is empty", name))
		}
	}
	if c.Routing.DefaultRoute != "" {
		if _, ok := c.Routes[c.Routing.DefaultRoute]; !ok {
			problems = append(problems, fmt.Sprintf("routing.default_route references missing route %q", c.Routing.DefaultRoute))
		}
	}
	for name, route := range c.Routes {
		ctx := fmt.Sprintf("route %q", name)
		switch route.Type {
		case "direct":
			if route.Target == "" && len(route.Candidates) == 0 {
				problems = append(problems, fmt.Sprintf("%s direct route requires target or candidates", ctx))
			}
			c.validateTargetRef(route.Target, ctx+".target", &problems)
			c.validateTargetRefs(route.Candidates, ctx+".candidates", &problems)
			c.validateTargetRefs(route.Fallbacks, ctx+".fallbacks", &problems)
		case "auto":
			if len(route.Candidates) == 0 && len(c.Targets) == 0 {
				problems = append(problems, fmt.Sprintf("%s auto route requires candidates or configured targets", ctx))
			}
			if route.MoARoute != "" {
				if _, ok := c.Routes[route.MoARoute]; !ok {
					problems = append(problems, fmt.Sprintf("%s references missing moa_route %q", ctx, route.MoARoute))
				}
			}
			c.validateTargetRefs(route.Candidates, ctx+".candidates", &problems)
			c.validateTargetRefs(route.Fallbacks, ctx+".fallbacks", &problems)
		case "moa":
			if route.Aggregator == "" {
				problems = append(problems, fmt.Sprintf("%s moa route requires aggregator", ctx))
			}
			if len(route.References) == 0 {
				problems = append(problems, fmt.Sprintf("%s moa route requires references", ctx))
			}
			c.validateTargetRef(route.Aggregator, ctx+".aggregator", &problems)
			c.validateTargetRefs(route.References, ctx+".references", &problems)
			c.validateTargetRefs(route.Fallbacks, ctx+".fallbacks", &problems)
		case "mov":
			if route.Flow != "" && !isSupportedMoVFlow(route.Flow) {
				problems = append(problems, fmt.Sprintf("%s uses unsupported mov flow %q", ctx, route.Flow))
			}
			if len(route.Stages) > 0 {
				problems = append(problems, fmt.Sprintf("%s mov stages are reserved and not executable in this release", ctx))
			}
			if len(movPrimaryTargets(route)) == 0 {
				problems = append(problems, fmt.Sprintf("%s mov route requires at least one target, candidate, reference, aggregator, or fallback", ctx))
			}
			c.validateTargetRef(route.Target, ctx+".target", &problems)
			c.validateTargetRefs(route.Candidates, ctx+".candidates", &problems)
			c.validateTargetRefs(route.References, ctx+".references", &problems)
			c.validateTargetRef(route.Aggregator, ctx+".aggregator", &problems)
			c.validateTargetRefs(route.Fallbacks, ctx+".fallbacks", &problems)
			for i, stage := range route.Stages {
				c.validateTargetRefs(stage.Targets, fmt.Sprintf("%s.stages[%d].targets", ctx, i), &problems)
			}
		case "passthrough":
			c.validatePassthroughRoute(name, ctx, &problems)
		default:
			problems = append(problems, fmt.Sprintf("%s has unsupported type %q", ctx, route.Type))
		}
		c.validateTargetRefs(route.ShadowTargets, ctx+".shadow_targets", &problems)
		for i, listener := range route.SerialListeners {
			c.validateTargetRef(listener.Target, fmt.Sprintf("%s.serial_listeners[%d].target", ctx, i), &problems)
			if listener.Mode != "" && listener.Mode != "serial" {
				problems = append(problems, fmt.Sprintf("%s.serial_listeners[%d].mode has unsupported value %q", ctx, i, listener.Mode))
			}
		}
		if route.Judge.Enabled {
			c.validateTargetRef(route.Judge.Target, ctx+".judge.target", &problems)
			c.validateTargetRefs(route.Judge.Candidates, ctx+".judge.candidates", &problems)
		}
		c.validateKeywordRules(route.KeywordRules, ctx, &problems)
		c.validateTargetRefs(route.Race.Targets, ctx+".race.targets", &problems)
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) validateKeywordRules(rules []KeywordRule, context string, problems *[]string) {
	for i, rule := range rules {
		ruleCtx := fmt.Sprintf("%s.keyword_rules[%d]", context, i)
		if rule.Require && !keywordRuleHasTargetSelector(rule) {
			*problems = append(*problems, fmt.Sprintf("%s require=true needs targets or tags", ruleCtx))
		}
		c.validateTargetRefs(rule.Targets, ruleCtx+".targets", problems)
	}
}

func (c Config) validateTargetRefs(names []string, context string, problems *[]string) {
	for _, name := range names {
		c.validateTargetRef(name, context, problems)
	}
}

func (c Config) validateTargetRef(name, context string, problems *[]string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if _, ok := c.Targets[name]; ok {
		return
	}
	if !strings.Contains(name, "/") {
		*problems = append(*problems, fmt.Sprintf("%s references missing target %q", context, name))
		return
	}
	switch normalizeUnknownModelPolicy(c.Routing.UnknownModelPolicy) {
	case "passthrough_openai":
		if _, ok := c.Providers["openai"]; ok {
			return
		}
	case "passthrough_openrouter":
		if _, ok := c.Providers["openrouter"]; ok {
			return
		}
	}
	*problems = append(*problems, fmt.Sprintf("%s references missing target %q", context, name))
}

func (c Config) validatePassthroughRoute(modelID, context string, problems *[]string) {
	if _, ok := c.Targets[modelID]; ok {
		return
	}
	switch normalizeUnknownModelPolicy(c.Routing.UnknownModelPolicy) {
	case "passthrough_openai":
		if _, ok := c.Providers["openai"]; ok {
			return
		}
	case "passthrough_openrouter":
		if _, ok := c.Providers["openrouter"]; ok {
			return
		}
	}
	*problems = append(*problems, fmt.Sprintf("%s passthrough route requires a configured target named %q or an explicit passthrough provider policy", context, modelID))
}

func configuredAPIKeys(auth AuthConfig) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, k := range auth.APIKeys {
		k = strings.TrimSpace(k)
		if k != "" {
			keys[k] = struct{}{}
		}
	}
	if auth.APIKeysEnv != "" {
		for _, k := range strings.Split(os.Getenv(auth.APIKeysEnv), ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				keys[k] = struct{}{}
			}
		}
	}
	return keys
}
