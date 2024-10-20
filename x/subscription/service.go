package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel/codes"
)

type service struct {
	repo   Repository
	entity core.EntityService
	policy core.PolicyService
}

// NewRepository creates a new collection repository
func NewService(
	repo Repository,
	entity core.EntityService,
	policy core.PolicyService,
) core.SubscriptionService {
	return &service{
		repo,
		entity,
		policy,
	}
}

// UpsertSubscription creates new collection
func (s *service) UpsertSubscription(ctx context.Context, mode core.CommitMode, document, signature string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.CreateSubscription")
	defer span.End()

	var doc core.SubscriptionDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	if doc.Owner == "" {
		doc.Owner = doc.Signer
	}

	if doc.ID == "" { // New
		hash := core.GetHash([]byte(document))
		hash10 := [10]byte{}
		copy(hash10[:], hash[:10])
		signedAt := doc.SignedAt
		doc.ID = cdid.New(hash10, signedAt).String()

		// check existance
		_, err := s.repo.GetSubscription(ctx, doc.ID)
		if err == nil {
			return core.Subscription{}, errors.New("subscription already exists")
		}

		policyResult, err := s.policy.TestWithPolicyURL(
			ctx,
			"",
			core.RequestContext{
				Requester: signer,
				Document:  doc,
			},
			"subscription.create",
		)
		if err != nil {
			span.RecordError(err)
		}

		result := s.policy.Summerize([]core.PolicyEvalResult{policyResult}, "subscription.create", nil)
		if !result {
			return core.Subscription{}, errors.New("You don't have subscription.create access")
		}

	} else {
		existance, err := s.repo.GetSubscription(ctx, doc.ID)
		if err != nil {
			span.RecordError(err)
			return core.Subscription{}, err
		}

		doc.Owner = existance.Owner // make sure the owner is immutable

		var params map[string]any = make(map[string]any)
		if existance.PolicyParams != nil {
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
				span.RecordError(err)
			}
		}

		policyResult, err := s.policy.TestWithPolicyURL(
			ctx,
			existance.Policy,
			core.RequestContext{
				Requester: signer,
				Self:      existance,
				Document:  doc,
				Params:    params,
			},
			"subscription.update",
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		result := s.policy.Summerize([]core.PolicyEvalResult{policyResult}, "subscription.update", nil)
		if !result {
			return core.Subscription{}, errors.New("You don't have subscription.update access")
		}
	}

	var policyparams *string = nil
	if doc.PolicyParams != "" {
		policyparams = &doc.PolicyParams
	}

	created, err := s.repo.CreateSubscription(ctx, core.Subscription{
		ID:           doc.ID,
		Owner:        doc.Owner,
		Author:       doc.Signer,
		Indexable:    doc.Indexable,
		Schema:       doc.Schema,
		Policy:       doc.Policy,
		PolicyParams: policyparams,
		Document:     document,
		Signature:    signature,
	})

	if err != nil {
		span.RecordError(err)
		return created, err
	}

	return created, nil
}

// GetSubscription returns a Subscription by ID
func (s *service) GetSubscription(ctx context.Context, id string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.GetSubscription")
	defer span.End()

	return s.repo.GetSubscription(ctx, id)
}

// DeleteSubscription deletes a collection by ID
func (s *service) DeleteSubscription(ctx context.Context, mode core.CommitMode, document string) (core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.DeleteSubscription")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	deleteTarget, err := s.repo.GetSubscription(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	signer, err := s.entity.Get(ctx, doc.Signer)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	var params map[string]any = make(map[string]any)
	if deleteTarget.PolicyParams != nil {
		json.Unmarshal([]byte(*deleteTarget.PolicyParams), &params)
	}

	policyResult, err := s.policy.TestWithPolicyURL(
		ctx,
		deleteTarget.Policy,
		core.RequestContext{
			Requester: signer,
			Self:      deleteTarget,
			Document:  doc,
		},
		"subscription.delete",
	)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	result := s.policy.Summerize([]core.PolicyEvalResult{policyResult}, "subscription.delete", nil)
	if !result {
		return core.Subscription{}, errors.New("policy failed")
	}

	err = s.repo.DeleteSubscription(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Subscription{}, err
	}

	return deleteTarget, err
}

// GetOwnSubscriptions returns all subscriptions owned by the owner
func (s *service) GetOwnSubscriptions(ctx context.Context, author string) ([]core.Subscription, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.GetOwnSubscriptions")
	defer span.End()

	return s.repo.GetSubscriptionsByAuthor(ctx, author)
}

// Subscribe creates new collection item
func (s *service) Subscribe(ctx context.Context, mode core.CommitMode, document, signature string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.Subscribe")
	defer span.End()

	var doc core.SubscribeDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	subscription, err := s.repo.GetSubscription(ctx, doc.Subscription)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	if doc.Signer != subscription.Author {
		return core.SubscriptionItem{}, errors.New("you are not authorized to perform this action")
	}

	fullID := doc.Target
	split := strings.Split(fullID, "@")
	if len(split) != 2 {
		return core.SubscriptionItem{}, errors.New("target must be in the format of id@resolver")
	}

	item := core.SubscriptionItem{
		ID:           doc.Target,
		Subscription: doc.Subscription,
	}

	resolver := split[1]
	if core.IsCCID(resolver) {
		item.Entity = &resolver
		item.ResolverType = core.ResolverTypeEntity
	} else { // web resolvation
		item.Domain = &resolver
		item.ResolverType = core.ResolverTypeDomain
	}

	created, err := s.repo.CreateItem(ctx, item)
	if err != nil {
		span.RecordError(err)
		return created, err
	}

	return created, nil
}

// DeleteItem deletes a collection item by ID
func (s *service) Unsubscribe(ctx context.Context, mode core.CommitMode, document string) (core.SubscriptionItem, error) {
	ctx, span := tracer.Start(ctx, "Subscription.Service.Unsubscribe")
	defer span.End()

	var doc core.UnsubscribeDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	item, err := s.repo.GetItem(ctx, doc.Target, doc.Subscription)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	subscription, err := s.repo.GetSubscription(ctx, doc.Subscription)
	if err != nil {
		span.RecordError(err)
		return core.SubscriptionItem{}, err
	}

	if doc.Signer != subscription.Author {
		return core.SubscriptionItem{}, errors.New("you are not authorized to perform this action")
	}

	err = s.repo.DeleteItem(ctx, doc.Target, doc.Subscription)

	return item, err
}

func (s *service) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Subscription.Service.Clean")
	defer span.End()

	subscriptions, err := s.repo.GetSubscriptionsByAuthorOwned(ctx, ccid)
	if err != nil {
		span.RecordError(err)
		return err
	}

	for _, subscription := range subscriptions {
		err := s.repo.DeleteSubscription(ctx, subscription.ID)
		if err != nil {
			span.RecordError(err)
			return err
		}
	}

	return nil
}
