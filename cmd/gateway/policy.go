package main

import (
	"encoding/json"

	"github.com/totegamma/concurrent/core"
)

var globalPolicyJson = `
{
    "statements": {
        "global": {
            "dominant": true,
            "defaultOnTrue": true,
            "condition": {
                "op": "Not",
                "args": [
                    {
                        "op": "Or",
                        "args": [
                            {
                                "op": "RequesterDomainHasTag",
                                "const": "_block"
                            },
                            {
                                "op": "RequesterHasTag",
                                "const": "_block"
                            }
                        ]
                    }
                ]
            }
        },
        "invite": {
            "condition": {
                "op": "RequesterHasTag",
                "const": "_invite"
            }
        },
        "timeline.distribute": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "LoadSelf",
                        "const": "domainOwned"
                    },
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "RequesterID"
                            }
                        ]
                    }
                ]
            }
        },
        "timeline.message.read": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "LoadSelf",
                        "const": "domainOwned"
                    },
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "RequesterID"
                            }
                        ]
                    }
                ]
            }
        }
    }
}`

func getDefaultGlobalPolicy() core.Policy {
	globalPolicy := core.Policy{}
	err := json.Unmarshal([]byte(globalPolicyJson), &globalPolicy)
	if err != nil {
		panic("failed to parse global policy")
	}

	return globalPolicy
}
