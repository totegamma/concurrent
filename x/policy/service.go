package policy

import (
	"context"
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

var tracer = otel.Tracer("policy")

type service struct {
	config util.Config
}

func NewService(config util.Config) core.PolicyService {
	return &service{config}
}

func (s service) Test(ctx context.Context, policy core.Policy, context core.RequestContext, action string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.Test")
	defer span.End()

	for _, statement := range policy.Statements {
		for _, a := range statement.Action {
			if isActionMatch(action, a) {
				result, err := s.eval(statement.Condition, context)
				if err != nil {
					return false, err
				}

				result_bool, ok := result.(bool)
				if !ok {
					return false, fmt.Errorf("bad argument type for OR. Expected bool but got %s\n", reflect.TypeOf(result).String())
				}

				if statement.Effect == "allow" {
					return result_bool, nil
				} else {
					return !result_bool, nil
				}
			}
		}
	}
	return false, nil
}

func (s service) HasNoRules(policy core.Policy, action string) (bool, error) {
	for _, statement := range policy.Statements {
		for _, a := range statement.Action {
			if isActionMatch(action, a) {
				return false, nil
			}
		}
	}
	return true, nil
}
