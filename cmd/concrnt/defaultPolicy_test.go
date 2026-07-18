package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/service"
	"github.com/concrnt/concrnt/policy"
)

func TestGlobalPolicyViaPolicyService(t *testing.T) {
	t.Parallel()

	policyService := service.NewPolicyService(
		GetGlobalPolicy(),
		service.GlobalParameters{FQDN: "local.example"},
		nil,
	)

	localRequester := domain.Entity{
		ID:     "con1localrequester",
		Domain: "local.example",
	}
	otherLocalRequester := domain.Entity{
		ID:     "con1otherlocal",
		Domain: "local.example",
	}
	remoteRequester := domain.Entity{
		ID:     "con1remote",
		Domain: "remote.example",
	}

	tests := []struct {
		name          string
		action        string
		requester     domain.Entity
		self          concrnt.Document[any]
		key           string
		expectAllowed bool
	}{
		{
			// ローカル登録ユーザーは、自分のnamespaceへの新規投稿をグローバルポリシーだけで許可される。
			name:          "record create allows own namespace",
			action:        "record:create",
			requester:     localRequester,
			key:           "cckv://con1localrequester/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1localrequester/timeline/post-1",
				Author: localRequester.ID,
			},
		},
		{
			// "## create - サーバーnamespaceについて - このサーバーの登録者であればOK"
			name:          "record create allows local requester on server namespace",
			action:        "record:create",
			requester:     localRequester,
			key:           "cckv://local.example/shared/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://local.example/shared/post-1",
				Author: localRequester.ID,
			},
		},
		{
			// 外部ユーザー自身のnamespaceは、このサーバーの登録ユーザーnamespaceではないのでデフォルトNGになる。
			name:          "record create denies remote requester even in own namespace",
			action:        "record:create",
			requester:     remoteRequester,
			key:           "cckv://con1remote/remote/post-1",
			expectAllowed: false,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1remote/remote/post-1",
				Author: remoteRequester.ID,
			},
		},
		{
			// "## create - デフォルトNG"
			name:          "record create denies other users namespace by default",
			action:        "record:create",
			requester:     localRequester,
			key:           "cckv://con1otherlocal/timeline/post-1",
			expectAllowed: false,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1otherlocal/timeline/post-1",
				Author: localRequester.ID,
			},
		},
		{
			// "## read - デフォルトOK"
			name:          "record read defaults to allowed",
			action:        "record:read",
			requester:     otherLocalRequester,
			key:           "cckv://con1localrequester/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1localrequester/timeline/post-1",
				Author: localRequester.ID,
			},
		},
		{
			// "## read - 自分のnamespaceであればALLOW"
			name:          "record read allows own namespace explicitly",
			action:        "record:read",
			requester:     localRequester,
			key:           "cckv://con1localrequester/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1localrequester/timeline/post-1",
				Author: otherLocalRequester.ID,
			},
		},
		{
			// "## update - 自分のnamespaceであればALLOW"
			name:          "record update allows own namespace",
			action:        "record:update",
			requester:     localRequester,
			key:           "cckv://con1localrequester/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1localrequester/timeline/post-1",
				Author: otherLocalRequester.ID,
			},
		},
		{
			// "## update - 自分が作成したリソースであればALLOW"
			name:          "record update allows author",
			action:        "record:update",
			requester:     localRequester,
			key:           "cckv://con1otherlocal/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1otherlocal/timeline/post-1",
				Author: localRequester.ID,
			},
		},
		{
			// "## update - デフォルトNG"
			name:          "record update denies otherwise",
			action:        "record:update",
			requester:     localRequester,
			key:           "cckv://con1otherlocal/timeline/post-1",
			expectAllowed: false,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1otherlocal/timeline/post-1",
				Author: otherLocalRequester.ID,
			},
		},
		{
			// "## delete - 自分のnamespaceであればALLOW"
			name:          "record delete allows own namespace",
			action:        "record:delete",
			requester:     localRequester,
			key:           "cckv://con1localrequester/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1localrequester/timeline/post-1",
				Author: otherLocalRequester.ID,
			},
		},
		{
			// "## delete - 自分が作成したリソースであればALLOW"
			name:          "record delete allows author",
			action:        "record:delete",
			requester:     localRequester,
			key:           "cckv://con1otherlocal/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1otherlocal/timeline/post-1",
				Author: localRequester.ID,
			},
		},
		{
			// "## delete - デフォルトNG"
			name:          "record delete denies otherwise",
			action:        "record:delete",
			requester:     localRequester,
			key:           "cckv://con1otherlocal/timeline/post-1",
			expectAllowed: false,
			self: concrnt.Document[any]{
				Kind:   "record",
				Key:    "cckv://con1otherlocal/timeline/post-1",
				Author: otherLocalRequester.ID,
			},
		},
		{
			// "## create - デフォルトOK"
			name:          "association create defaults to allowed",
			action:        "association:create",
			requester:     localRequester,
			key:           "cckv://con1otherlocal/timeline/post-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:      "association",
				Author:    localRequester.ID,
				Associate: ptr("cckv://con1otherlocal/timeline/post-1"),
			},
		},
		{
			// "## read - デフォルトOK"
			name:          "association read defaults to allowed",
			action:        "association:read",
			requester:     localRequester,
			key:           "ccfs://con1otherlocal/concrnt/assoc-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:      "association",
				Author:    otherLocalRequester.ID,
				Associate: ptr("cckv://con1otherlocal/timeline/post-1"),
			},
		},
		{
			// "## delete - 親が自分のnamespaceであればALLOW"
			name:          "association delete allows parent namespace owner",
			action:        "association:delete",
			requester:     localRequester,
			key:           "ccfs://con1localrequester/concrnt/assoc-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:      "association",
				Author:    otherLocalRequester.ID,
				Associate: ptr("cckv://con1localrequester/timeline/post-1"),
			},
		},
		{
			// "## delete - 自分が作成したassociationであればALLOW"
			name:          "association delete allows association author",
			action:        "association:delete",
			requester:     localRequester,
			key:           "ccfs://con1otherlocal/concrnt/assoc-1",
			expectAllowed: true,
			self: concrnt.Document[any]{
				Kind:      "association",
				Author:    localRequester.ID,
				Associate: ptr("cckv://con1otherlocal/timeline/post-1"),
			},
		},
		{
			// "## delete - デフォルトNG"
			name:          "association delete denies otherwise",
			action:        "association:delete",
			requester:     localRequester,
			key:           "ccfs://con1otherlocal/concrnt/assoc-1",
			expectAllowed: false,
			self: concrnt.Document[any]{
				Kind:      "association",
				Author:    otherLocalRequester.ID,
				Associate: ptr("cckv://con1otherlocal/timeline/post-1"),
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := policyService.Eval(
				context.Background(),
				policy.RequestContext{
					Requester: tt.requester,
					Self:      tt.self,
				},
				nil,
				tt.action,
				tt.key,
			)

			if tt.expectAllowed {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, domain.ErrPermissionDenied))
			var permErr domain.PermissionError
			require.ErrorAs(t, err, &permErr)
		})
	}
}

func TestGlobalPolicyAllowsRemoteCreateWhenDescendantPolicyAllows(t *testing.T) {
	t.Parallel()

	remoteRequester := domain.Entity{
		ID:     "con1remote",
		Domain: "remote.example",
	}
	self := concrnt.Document[any]{
		Kind:   "record",
		Key:    "cckv://con1localrequester/open/post-1",
		Author: remoteRequester.ID,
	}

	// 外部ユーザーはグローバルポリシーでは確定拒否されず、
	// 登録ユーザー配下の下位ポリシーがrecord:createを明示許可すれば投稿できる。
	conclusion, _, err := policy.EvaluateStack(
		context.Background(),
		policy.RequestContext{
			Requester: remoteRequester,
			Self:      self,
			Globals:   service.GlobalParameters{FQDN: "local.example"},
		},
		policy.PolicyStack{
			{
				{Policy: GetGlobalPolicy()},
			},
			{
				{
					Policy: policy.Policy{
						Statements: []policy.Statement{
							{
								Action:    "record:create",
								Key:       "cckv://con1localrequester/open/*",
								Emit:      policy.ALLOW,
								Condition: policy.Expr{Operator: "Const", Const: true},
							},
						},
					},
				},
			},
		},
		"record:create",
		self.Key,
	)

	require.NoError(t, err)
	require.Equal(t, policy.ALLOW, conclusion)
}

func TestGlobalPolicyDeniesRemoteCreateWithoutDescendantPolicyAllow(t *testing.T) {
	t.Parallel()

	remoteRequester := domain.Entity{
		ID:     "con1remote",
		Domain: "remote.example",
	}
	self := concrnt.Document[any]{
		Kind:   "record",
		Key:    "cckv://con1localrequester/open/post-1",
		Author: remoteRequester.ID,
	}

	// 下位レイヤーが存在しても、record:createへの許可が明示されていなければ
	// グローバルポリシーのデフォルトNGが残り、外部ユーザーの投稿は拒否される。
	conclusion, _, err := policy.EvaluateStack(
		context.Background(),
		policy.RequestContext{
			Requester: remoteRequester,
			Self:      self,
			Globals:   service.GlobalParameters{FQDN: "local.example"},
		},
		policy.PolicyStack{
			{
				{Policy: GetGlobalPolicy()},
			},
			{
				{
					Policy: policy.Policy{
						Statements: []policy.Statement{
							{
								// record:readの許可はrecord:createには効かないことを確認する。
								Action:    "record:read",
								Key:       "cckv://con1localrequester/open/*",
								Emit:      policy.ALLOW,
								Condition: policy.Expr{Operator: "Const", Const: true},
							},
						},
					},
				},
			},
		},
		"record:create",
		self.Key,
	)

	require.NoError(t, err)
	require.Equal(t, policy.NG, conclusion)
}

func ptr[T any](v T) *T {
	return &v
}
