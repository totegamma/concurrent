package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/policy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GlobalParameters struct {
	FQDN string `json:"fqdn"`
}

type PolicyService struct {
	globalPolicy     policy.Policy
	globalParameters GlobalParameters
	client           *client.Client
	cache            *cache.Cache
}

func NewPolicyService(
	globalPolicy policy.Policy,
	globalParameters GlobalParameters,
	client *client.Client,
) *PolicyService {
	return &PolicyService{
		globalPolicy:     globalPolicy,
		globalParameters: globalParameters,
		client:           client,
		cache:            cache.New(10*time.Minute, 15*time.Minute),
	}
}

// policyAllowedAPIs is the in-code allowlist of named concrnt APIs that
// policy ConcrntCall expressions may invoke. Checked before any resolution
// or network I/O.
var policyAllowedAPIs = []string{
	"net.concrnt.core.acknowledges",
}

func (s *PolicyService) ConcrntCall(ctx context.Context, resolver string, api string, params map[string]string) (any, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.ConcrntCall")
	defer span.End()

	if !slices.Contains(policyAllowedAPIs, api) {
		err := fmt.Errorf("api %q is not allowed in policy evaluation", api)
		span.RecordError(err)
		return nil, err
	}

	var result any
	err := s.client.Call(ctx, resolver, api, params, nil, &result)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	// List endpoints return the paged envelope (CIP-5 §3.2); policy
	// expressions like IsNotEmpty operate on the item list (CIP-12 §4.2.5).
	if envelope, ok := result.(map[string]any); ok {
		if items, ok := envelope["items"]; ok {
			return items, nil
		}
	}

	return result, nil
}

func (s *PolicyService) ResolvePolicyURL(ctx context.Context, policyURL string) (policy.Policy, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.ResolvePolicyURL")
	defer span.End()

	cached, found := s.cache.Get(policyURL)
	if found {
		var policy20251223 policy.Policy
		err := json.Unmarshal(cached.([]byte), &policy20251223)
		if err != nil {
			span.RecordError(err)
			s.cache.Delete(policyURL) // remove corrupted cache
			return policy.Policy{}, err
		}
		return policy20251223, nil
	}

	resp, err := http.Get(policyURL)
	if err != nil {
		span.RecordError(err)
		return policy.Policy{}, err
	}
	defer resp.Body.Close()

	var policyDoc policy.PolicyDocument
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&policyDoc)
	if err != nil {
		span.RecordError(err)
		return policy.Policy{}, err
	}

	policyAny, ok := policyDoc.Versions["2025-12-23"]
	if !ok {
		return policy.Policy{}, fmt.Errorf("unsupported policy version in %s", policyURL)
	}

	policyBytes, err := json.Marshal(policyAny)
	if err != nil {
		span.RecordError(err)
		return policy.Policy{}, err
	}

	var policy20251223 policy.Policy
	err = json.Unmarshal(policyBytes, &policy20251223)
	if err != nil {
		span.RecordError(err)
		return policy.Policy{}, err
	}

	s.cache.Set(policyURL, policyBytes, cache.DefaultExpiration)

	return policy20251223, nil
}

func (s *PolicyService) resolvePolicyStack(ctx context.Context, stack []concrnt.Policy) (policy.PolicyStack, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.resolvePolicyStack")
	defer span.End()

	// CIP-12 §5.3: a virtual parent (the declaring resource's distribution
	// destinations) forms its own layer inserted immediately before the layer
	// that declares it — one layer per hierarchy level, never merged into an
	// ancestor's. Its Source is the declaring layer's parent path, so the
	// referenced policy's relative keys ('.', '*', './*') rewrite to a prefix
	// that matches the evaluated record key.
	resolved := make([]concrnt.Policy, 0, len(stack))
	for _, layer := range stack {
		if layer.VirtualParents != nil && len(*layer.VirtualParents) > 0 {
			split := strings.Split(layer.Source, "/")
			if len(split) == 0 {
				span.AddEvent("invalid policy source format", trace.WithAttributes(attribute.String("source", layer.Source)))
			} else {
				virtual := concrnt.Policy{Source: strings.Join(split[:len(split)-1], "/")}
				for _, parent := range *layer.VirtualParents {
					var doc concrnt.Document[any]
					// The virtual-parent policy record itself is not sensitive data
					// used for anything but building the evaluation stack, so strict
					// signature verification is unnecessary here. It is cached (10-min
					// resource TTL): every commit distributed to a timeline evaluates
					// this policy, so an uncached fetch here means one HTTP round trip
					// per distribution — bulk imports would otherwise hammer it (and,
					// via gateway remapping, the server itself). A timeline policy
					// change taking up to the cache TTL to apply is acceptable.
					err := s.client.GetRecord(ctx, parent, &client.Options{SkipVerify: true}, &doc)
					if err != nil {
						span.RecordError(err)
						virtual.Entries = append(virtual.Entries, concrnt.PolicyEntry{Errored: true})
						continue
					}

					if doc.Policy == nil {
						span.AddEvent("policy reference has no policies", trace.WithAttributes(attribute.String("ref", parent)))
						continue
					}

					virtual.Entries = append(virtual.Entries, doc.Policy.Entries...)
				}
				if len(virtual.Entries) > 0 {
					resolved = append(resolved, virtual)
				}
			}
		}
		resolved = append(resolved, layer)
	}

	result := policy.PolicyStack{}

	for _, layer := range resolved {
		policyLayer := []policy.EvaluationSet{}
		for _, p := range layer.Entries {

			if p.Errored {
				policyLayer = append(policyLayer, erroredEvaluationSet(p))
				continue
			}

			if p.URL != nil {
				pol, err := s.ResolvePolicyURL(ctx, *p.URL)
				if err != nil {
					span.RecordError(err)
					// mark this layer has errored policy
					policyLayer = append(policyLayer, erroredEvaluationSet(p))
					continue
				}

				for i := range pol.Statements {
					switch pol.Statements[i].Key {
					case ".": // this only
						pol.Statements[i].Key = layer.Source
					case "", "*": // this and all children
						pol.Statements[i].Key = layer.Source + "*"
					case "./*": // all children but not this
						pol.Statements[i].Key = layer.Source + "/*"
					}
				}

				// Entry-level defaults override the resolved policy's own,
				// so the referencing document decides the fallback conclusion.
				if defaults := entryDefaults(p); defaults != nil {
					if pol.Defaults == nil {
						pol.Defaults = defaults
					} else {
						for action, conclusion := range defaults {
							pol.Defaults[action] = conclusion
						}
					}
				}

				policyLayer = append(policyLayer, policy.EvaluationSet{
					Policy: pol,
					Params: p.Params,
				})
			}
		}
		result = append(result, policyLayer)
	}

	return result, nil
}

// entryDefaults converts a policy entry's per-action default conclusions
// (how the author wants evaluation to fall back when the referenced policy
// can't be resolved or doesn't decide) into evaluator form.
func entryDefaults(p concrnt.PolicyEntry) map[string]policy.Conclusion {
	if p.Defaults == nil {
		return nil
	}
	defaults := make(map[string]policy.Conclusion, len(*p.Defaults))
	for action, conclusion := range *p.Defaults {
		defaults[action] = policy.Conclusion(conclusion)
	}
	return defaults
}

// erroredEvaluationSet marks an unresolvable policy entry for evaluation:
// EvaluateStack folds it to the entry's declared default conclusion (or
// UNSET when the entry declares none), letting policy authors choose
// fail-open vs fail-closed per action instead of erroring out entirely.
func erroredEvaluationSet(p concrnt.PolicyEntry) policy.EvaluationSet {
	return policy.EvaluationSet{
		Errored: true,
		Policy:  policy.Policy{Defaults: entryDefaults(p)},
	}
}

func (s *PolicyService) Eval(ctx context.Context, req policy.RequestContext, stack []concrnt.Policy, action string, key string) error {
	ctx, span := tracer.Start(ctx, "Policy.Service.Eval")
	defer span.End()

	var policyStack policy.PolicyStack
	policyStack = append(policyStack, []policy.EvaluationSet{
		{
			Policy: s.globalPolicy,
		},
	})

	additionalStack, err := s.resolvePolicyStack(ctx, stack)
	if err != nil {
		span.RecordError(err)
		return err
	}

	policyStack = append(policyStack, additionalStack...)

	requestContext := policy.RequestContext{
		Requester: req.Requester,
		Self:      req.Self,
		Params:    req.Params,
		Globals:   s.globalParameters,
		Caller:    s,
	}

	conclusion, reason, error := policy.EvaluateStack(ctx, requestContext, policyStack, action, key)
	if error != nil {
		return error
	}

	switch conclusion {
	case policy.ALLOW, policy.OK:
		return nil
	default:
		return domain.PermissionError{Reason: "action denied by policy: " + reason}
	}
}
