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
        "association.delete": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "LoadDocument",
                                "const": "signer"
                            }
                        ]
                    },
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "owner"
                            },
                            {
                                "op": "LoadDocument",
                                "const": "signer"
                            }
                        ]
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "_admin"
                    }
                ]
            }
        },
        "message.delete": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "LoadDocument",
                                "const": "signer"
                            }
                        ]
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "_admin"
                    }
                ]
            }
        },
        "profile.delete": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "LoadDocument",
                                "const": "signer"
                            }
                        ]
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "_admin"
                    }
                ]
            }
        },
        "subscription.create": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "IsRequesterLocalUser"
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "subscription_creator"
                    }
                ]
            }
        },
        "subscription.update": {
            "condition": {
                "op": "Eq",
                "args": [
                    {
                        "op": "LoadSelf",
                        "const": "author"
                    },
                    {
                        "op": "LoadDocument",
                        "const": "signer"
                    }
                ]
            }
        },
        "subscription.delete": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "LoadDocument",
                                "const": "signer"
                            }
                        ]
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "_admin"
                    }
                ]
            }
        },
        "timeline.create": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "IsRequesterLocalUser"
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "timeline_creator"
                    }
                ]
            }
        },
        "timeline.update": {
            "condition": {
                "op": "Eq",
                "args": [
                    {
                        "op": "LoadSelf",
                        "const": "author"
                    },
                    {
                        "op": "LoadDocument",
                        "const": "signer"
                    }
                ]
            }
        },
        "timeline.delete": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "LoadDocument",
                                "const": "signer"
                            }
                        ]
                    },
                    {
                        "op": "RequesterHasTag",
                        "const": "_admin"
                    }
                ]
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
    },
    "defaults": {
        "association.attach": true
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
