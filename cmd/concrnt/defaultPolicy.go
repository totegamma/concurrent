package main

import (
	"encoding/json"

	"github.com/concrnt/concrnt/policy"
)

/* Global Policy
# Record (record:)
## create
- デフォルトNG
- このサーバーの登録者による新規投稿のkeyのnamespaceが、投稿者自身のものであればALLOW
- それ以外は、下位階層のポリシーで許可されなければNG
- サーバーnamespaceについて
  - このサーバーの登録者であればOK

## read
- デフォルトOK
- 自分のnamespaceであればALLOW

## update
- デフォルトNG
- 自分のnamespaceであればALLOW
- 自分が作成したリソースであればALLOW

## delete
- デフォルトNG
- 自分のnamespaceであればALLOW
- 自分が作成したリソースであればALLOW

# Association (association:)
## create
- デフォルトOK

## read
- デフォルトOK

## delete
- デフォルトNG
- 親が自分のnamespaceであればALLOW
- 自分が作成したassociationであればALLOW
*/

var globalPolicyJson = `
{
	"defaults": {
		"record:create": "ng",
		"record:read": "ok",
		"record:update": "ng",
		"record:delete": "ng",
		"association:create": "ok",
		"association:read": "ok",
		"association:delete": "ng"
	},
	"statements": [
		{
			"action": "record:create",
			"key": "*",
			"emit": "allow",
			"reason": "ALLOW if the record is created under the namespace of the requester",
			"condition": {
				"op": "And",
				"args": [
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.domain"
							},
							{
								"op": "Load",
								"const": "globals.fqdn"
							}
						]
					},
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "CCUriOwner",
								"args": [
									{
										"op": "Load",
										"const": "self.key"
									}
								]
							}
						]
					}
				]
			}
		},
		{
			"action": "record:create",
			"key": "*",
			"emit": "ok",
			"reason": "OK if the record is created under the namespace of this server",
			"condition": {
				"op": "And",
				"args": [
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.domain"
							},
							{
								"op": "Load",
								"const": "globals.fqdn"
							}
						]
					},
					{
						"op": "Eq",
						"args": [
							{
								"op": "CCUriOwner",
								"args": [
									{
										"op": "Load",
										"const": "self.key"
									}
								]
							},
							{
								"op": "Load",
								"const": "globals.fqdn"
							}
						]
					}
				]
			}
		},
		{
			"action": "record:read",
			"key": "*",
			"emit": "allow",
			"reason": "ALLOW if the record is in the namespace of the requester",
			"condition": {
				"op": "Eq",
				"args": [
					{
						"op": "Load",
						"const": "requester.ccid"
					},
					{
						"op": "CCUriOwner",
						"args": [
							{
								"op": "Load",
								"const": "self.key"
							}
						]
					}
				]
			}
		},
		{
			"action": "record:update",
			"key": "*",
			"emit": "allow",
			"reason": "ALLOW if the requester is the author of the record or the record is in the namespace of the requester",
			"condition": {
				"op": "Or",
				"args": [
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "Load",
								"const": "self.author"
							}
						]
					},
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "CCUriOwner",
								"args": [
									{
										"op": "Load",
										"const": "self.key"
									}
								]
							}
						]
					}
				]
			}
		},
		{
			"action": "record:delete",
			"key": "*",
			"emit": "allow",
			"reason": "ALLOW if the requester is the author of the record or the record is in the namespace of the requester",
			"condition": {
				"op": "Or",
				"args": [
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "Load",
								"const": "self.author"
							}
						]
					},
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "CCUriOwner",
								"args": [
									{
										"op": "Load",
										"const": "self.key"
									}
								]
							}
						]
					}
				]
			}
		},
		{
			"action": "association:delete",
			"key": "*",
			"emit": "allow",
			"reason": "ALLOW if the requester is the author of the association or the parent of the association is in the namespace of the requester",
			"condition": {
				"op": "Or",
				"args": [
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "Load",
								"const": "self.author"
							}
						]
					},
					{
						"op": "Eq",
						"args": [
							{
								"op": "Load",
								"const": "requester.ccid"
							},
							{
								"op": "CCUriOwner",
								"args": [
									{
										"op": "Load",
										"const": "self.associate"
									}
								]
							}
						]
					}
				]
			}
		}
	]
}`

func GetGlobalPolicy() policy.Policy {
	globalPolicy := policy.Policy{}
	err := json.Unmarshal([]byte(globalPolicyJson), &globalPolicy)
	if err != nil {
		panic("failed to parse global policy:" + err.Error())
	}

	return globalPolicy
}
