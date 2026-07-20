![wordmark](https://worldfile.cc/CC2d97694D850Df2089F48E639B4795dD95D2DCE2E/f696009d-f1f0-44f8-83fe-6387946f1b86)
### Concrnt: Makes social media accounts your internet identities.

[日本語](README-ja.md)

## What is Concrnt:
Concrnt is a distributed microblogging platform.

## Why Concrnt:
Using a social media account as a user identity, it's untenable to rely on centralized social media platforms that are prone to third-party censorship and irreversible account suspension. It's unacceptable for a carefully nurtured account to be suddenly frozen and rendered irrecoverable.

However, as long as servers physically exist, their operators must abide by the laws of the countries they are located in. Since operators are human, it's unrealistic to expect that accounts can never be frozen.

Concrnt resolves this dilemma. It uses a unique protocol designed to allow account migration. This means that even if your account is frozen on the server where it was initially created, you can move your account to another server and resume using it there, maintaining all past posts and friend connections just as before the freeze.

## How Concrnt solves the problem:
Concrnt uses public-key cryptography to verify user identities. Every piece of content you publish is a signed document, so anyone can verify that it really came from you — no matter which server happens to be storing it. It doesn't share any confidential information with servers, and users are responsible for managing the security of their own accounts.

## Highlights:
- **Account migration by design** — Your identity is your key pair, not a row in someone's database. Export your entire history of signed documents and replay it on a new server, and your account lives on with all posts and relationships intact.
- **Community timelines across servers** — Create any number of topic-based community timelines on each server. Unlike "local timelines" walled inside a single server, users from other servers can view and join them.
- **A small, composable protocol** — The protocol is defined as a set of small, independent specifications ([CIPs — Concrnt Improvement Proposals](https://github.com/concrnt/CIPs-translated)). It is deliberately kept simple enough for third parties to reimplement: a compliant server can be as minimal as a static file host paired with a small writer (even a serverless function).
- **Programmable permissions** — Access control is expressed with a policy engine, so communities and users can define fine-grained, customizable rules about who can read, write, or join, instead of being limited to a fixed set of visibility options.
- **Extensible via modules** — Additional features run as separate services that register themselves with the server at runtime, so a server can grow capabilities without forking the core.

## Cool! Where can I join?
You can experience the world of Concrnt through one of its web client implementations, available at [concrnt.world](https://concrnt.world)!

# Comparison

## Centralized (Twitter):
There's a fear of unjust suspension, and if an account is suspended, it signifies the death of that internet identity.

## Activitypub (Mastodon, Misskey):
While there is an account migration feature, if your account is suspended before migration, you're out of options. Additionally, major ActivityPub-supported social media platforms have a concept known as local timelines, which are not accessible or joinable by external users, ultimately forcing users to create new accounts on each server. Concrnt, however, allows for the creation of any number of topic-based community timelines on each server, which can be viewed and joined by users from other servers.

## nostr:
Nostr is a fantastic mechanism for proving one's identity using a private key, but it requires careful selection of relay servers. There is no guarantee that a relay server will retain or delete your data. (It works very well for use cases that do not require such assurances.) Due to its fundamentally decentralized nature, it seems unlikely to implement non-essential but convenient features (such as visibility control for non-encrypted messages).

## Bluesky:
Bluesky shares many similarities with Concrnt. Bluesky appears to be created with a mission similar to Twitter's, aimed at "making all the information in the world shareable." In contrast, Concrnt's mission focuses on "centering around communities and loosely connecting with the world." This difference in mission leads Bluesky to adopt an architecture that builds a massive index server and generates feeds, whereas Concrnt uses a single server to lightly and in real-time collect information from nearby sources.

## The problem of the Concrnt?:
The architectural strategy adopted by Concrnt may not scale to an ultra-large system where countless users can follow as many others as they like, similar to Twitter. This is because, rather than constructing a single, massive home timeline, it is designed with the assumption that users will create and switch between several moderately sized lists.

This approach is because Concrnt does not primarily aim to intensely connect with the world but to build relationships based on community timelines, optimizing for this purpose. This allows each domain to control information independently without the need for a massive server like an index server.

Furthermore, Concrnt embraces the concept of "protecting one's identity by oneself." However, in reality, this requires a certain level of knowledge and expertise, meaning there is an inherent complexity and difficulty in use.

In this sense, centralized social networks, where one relies on others for protection while trying to stay in the good graces of the administration to avoid being frozen, might be more reassuring for the average person.

# For geeks
## How to launch own server
look at detailed documentation: [concrnt square](https://square.concrnt.net/getting-started/hosting/)

To try a full stack locally (server + Postgres + Redis + memcached + web UI):

```sh
docker compose up
```

## Protocol specification
The protocol is specified as a collection of small documents called [CIPs (Concrnt Improvement Proposals)](https://github.com/concrnt/CIPs-translated).

## Contributing
When creating a PR, we generally recommend creating an issue first and reaching a consensus on whether or not to proceed. (Concrnt is currently being heavily developed, and there may be changes that cannot be made due to its policy.)

---

> [!IMPORTANT]
> The specification has been significantly updated since v1.10.0. If you are running an older version of the server, please follow the [migration guide](https://square.concrnt.net/operator/migration/) to migrate.
