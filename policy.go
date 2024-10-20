package concurrent

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
            "dominant": true,
            "defaultOnFalse": true,
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
            "dominant": true,
            "defaultOnFalse": true,
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
        "profile.create": {
            "condition": {
                "op": "IsRequesterLocalUser"
            }
        },
        "profile.update": {
            "dominant": true,
            "defaultOnFalse": true,
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
        "profile.delete": {
            "dominant": true,
            "defaultOnFalse": true,
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
            "dominant": true,
            "defaultOnFalse": true,
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
            "dominant": true,
            "defaultOnFalse": true,
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
            "dominant": true,
            "defaultOnFalse": true,
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
            "dominant": true,
            "defaultOnFalse": true,
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
                        "op": "IsCSID",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "owner"
                            }
                        ]
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
                    },
                    {
                        "op": "LoadSelf",
                        "const": "domainOwned"
                    }
                ]
            }
        },
        "timeline.retract": {
            "dominant": true,
            "defaultOnFalse": true,
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
                                "op": "LoadResource",
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
                                "op": "LoadResource",
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
        "timeline.message.read": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "IsCSID",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "owner"
                            }
                        ]
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
                    },
                    {
                        "op": "LoadSelf",
                        "const": "domainOwned"
                    }
                ]
            }
        }
    },
    "defaults": {
        "timeline.message.read": true,
        "message.association.attach": true,
        "timeline.association.attach": true,
        "subscription.association.attach": true
    }
}`

func GetDefaultGlobalPolicy() core.Policy {
	globalPolicy := core.Policy{}
	err := json.Unmarshal([]byte(globalPolicyJson), &globalPolicy)
	if err != nil {
		panic("failed to parse global policy")
	}

	return globalPolicy
}
