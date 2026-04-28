package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/client"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/policy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type PolicyService struct {
	global policy.Policy
	client *client.Client
	cache  *cache.Cache
}

func NewPolicyService(global policy.Policy, client *client.Client) *PolicyService {
	return &PolicyService{
		global: global,
		client: client,
		cache:  cache.New(10*time.Minute, 15*time.Minute),
	}
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

	var prepend *concrnt.Policy

	// insert virtual parent
	for i, layer := range stack {
		if layer.VirtualParents == nil {
			continue
		}

		for _, parent := range *layer.VirtualParents {
			var doc concrnt.Document[any]
			err := s.client.GetRecord(ctx, parent, &client.Options{NoCache: true}, &doc)
			if err != nil {
				span.RecordError(err)
				// TODO: insert a errored policy layer to indicate this error
				continue
			}

			if doc.Policy == nil {
				span.AddEvent("policy reference has no policies", trace.WithAttributes(attribute.String("ref", parent)))
				continue
			}

			if i == 0 {
				if prepend == nil {
					// generate parent url
					split := strings.Split(layer.Source, "/")
					if len(split) == 0 {
						span.AddEvent("invalid policy source format", trace.WithAttributes(attribute.String("source", layer.Source)))
						continue
					}

					parentURL := strings.Join(split[:len(split)-1], "/")

					prepend = &concrnt.Policy{
						Source:  parentURL,
						Entries: doc.Policy.Entries,
					}
				} else {
					prepend.Entries = append(prepend.Entries, doc.Policy.Entries...)
				}
			} else {
				entries := doc.Policy.Entries
				stack[i-1].Entries = append(stack[i-1].Entries, entries...)
			}
		}
	}

	if prepend != nil {
		stack = append([]concrnt.Policy{*prepend}, stack...)
	}

	result := policy.PolicyStack{}

	for _, layer := range stack {
		policyLayer := []policy.EvaluationSet{}
		for _, p := range layer.Entries {

			if p.URL != nil {
				pol, err := s.ResolvePolicyURL(ctx, *p.URL)
				if err != nil {
					span.RecordError(err)
					// mark this layer has errored policy
					policyLayer = append(policyLayer, policy.EvaluationSet{
						Errored:  true,
						Defaults: p.Defaults,
					})
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

				policyLayer = append(policyLayer, policy.EvaluationSet{
					Policy:   pol,
					Params:   p.Params,
					Defaults: p.Defaults,
				})
			}
		}
		result = append(result, policyLayer)
	}

	return result, nil
}

func (s *PolicyService) Eval(ctx context.Context, req policy.RequestContext, stack []concrnt.Policy, action string, key string) error {
	ctx, span := tracer.Start(ctx, "Policy.Service.Eval")
	defer span.End()

	var policyStack policy.PolicyStack
	policyStack = append(policyStack, []policy.EvaluationSet{
		{
			Policy: s.global,
		},
	})

	additionalStack, err := s.resolvePolicyStack(ctx, stack)
	if err != nil {
		span.RecordError(err)
		return err
	}

	policyStack = append(policyStack, additionalStack...)

	conclusion, error := policy.EvaluateStack(ctx, req, policyStack, action, key)
	if error != nil {
		return error
	}

	switch conclusion {
	case policy.ALLOW, policy.OK:
		return nil
	default:
		return domain.PermissionError{Reason: "action denied by policy"}
	}
}
