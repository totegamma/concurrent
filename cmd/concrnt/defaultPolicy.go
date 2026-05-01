package main

import (
	"encoding/json"

	"github.com/concrnt/concrnt/policy"
)

/* Global Policy


# Record (record:)
## create
- デフォルトNG
- 自分のnamespaceであればALLOW
- namespaceが登録ユーザーでなければDENY
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

---

登録ユーザーかどうかの判定:
	globalsにこのサーバーのfqdnが入っているので、それがrequesterのfqdnと一致するかどうかで判定する。


*/

var globalPolicyJson = `
{
	"statements": [
		{
			"action": "record:delete",
			"key": "*",
			"emit": "allow",
			"condition": {
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
			}
		},
		{
			"action": "record:read",
			"key": "*",
			"emit": "ok",
			"condition": {
				"op": "Const",
				"const": true
			}
		},
		{
			"action": "record:read",
			"key": "*",
			"emit": "allow",
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
