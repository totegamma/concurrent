package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

var tracer = otel.Tracer("policy")

type service struct {
	repository Repository
	config     util.Config
}

func NewService(repository Repository, config util.Config) core.PolicyService {
	return &service{
		repository,
		config,
	}
}

func (s service) TestWithPolicyURL(ctx context.Context, url string, context core.RequestContext, action string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.TestWithPolicyURL")
	defer span.End()

	policy, err := s.repository.Get(ctx, url)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}

	return s.Test(ctx, policy, context, action)
}

func (s service) Test(ctx context.Context, policy core.Policy, context core.RequestContext, action string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.Test")
	defer span.End()

	span.SetAttributes(attribute.String("action", action))

	for _, statement := range policy.Statements {
		_, span := tracer.Start(ctx, "Policy.Service.Test.Statement")
		for _, a := range statement.Actions {
			span.SetAttributes(attribute.StringSlice("actions", statement.Actions))
			if isActionMatch(action, a) {
				span.SetAttributes(attribute.Bool("examined", true))

				result, err := s.eval(statement.Condition, context)
				resultJson, _ := json.MarshalIndent(result, "", "  ")
				span.SetAttributes(attribute.String("result", string(resultJson)))
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					span.End()
					return false, err
				}

				result_bool, ok := result.Result.(bool)
				if !ok {
					err := fmt.Errorf("bad argument type for Policy. Expected bool but got %s\n", reflect.TypeOf(result).String())
					span.SetStatus(codes.Error, err.Error())
					span.End()
					return false, err
				}

				if statement.Effect == "allow" {
					span.End()
					return result_bool, nil
				} else {
					span.End()
					return !result_bool, nil
				}
			}
		}
		span.SetAttributes(attribute.Bool("examined", false))
		span.End()
	}
	return false, nil
}

func (s service) HasNoRulesWithPolicyURL(ctx context.Context, url string, action string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.HasNoRulesWithPolicyURL")
	defer span.End()

	policy, err := s.repository.Get(ctx, url)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}

	return s.HasNoRules(ctx, policy, action)
}

func (s service) HasNoRules(ctx context.Context, policy core.Policy, action string) (bool, error) {
	for _, statement := range policy.Statements {
		for _, a := range statement.Actions {
			if isActionMatch(action, a) {
				return false, nil
			}
		}
	}
	return true, nil
}
