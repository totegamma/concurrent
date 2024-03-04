![wordmark](https://github.com/totegamma/concurrent/assets/7270849/44864682-ca2d-427a-b1fb-552ca50bcfce)
### Makes social media accounts your internet identities.

[日本語](README-ja.md)

## What is Concurrent:
Concurrent is a distributed microblogging platform.

## Why Concurrent:
Using a social media account as a user identity, it's untenable to rely on centralized social media platforms that are prone to third-party censorship and irreversible account suspension. It's unacceptable for a carefully nurtured account to be suddenly frozen and rendered irrecoverable.

However, as long as servers physically exist, their operators must abide by the laws of the countries they are located in. Since operators are human, it's unrealistic to expect that accounts can never be frozen.

Concurrent resolves this dilemma. It uses a unique protocol designed to allow account migration. This means that even if your account is frozen on the server where it was initially created, you can move your account to another server and resume using it there, maintaining all past posts and friend connections just as before the freeze.

## How Concurrent solves the problem:
Concurrent uses public-key cryptography to verify user identities. It doesn't share any confidential information with servers, and users are responsible for managing the security of their own accounts.

## Cool! Where can I join?
You can experience the world of Concurrent through one of its web client implementations, available at [concurrent.world](https://concurrent.world)!

# Comparison

## Centralized (Twitter):
There's a fear of unjust suspension, and if an account is suspended, it signifies the death of that internet identity.

## Activitypub (Mastodon, Misskey):
While there is an account migration feature, if your account is suspended before migration, you're out of options. Additionally, major ActivityPub-supported social media platforms have a concept known as local timelines, which are not accessible or joinable by external users, ultimately forcing users to create new accounts on each server. Concurrent, however, allows for the creation of any number of topic-based community timelines on each server, which can be viewed and joined by users from other servers.

## nostr:
It's a great mechanism that uses private keys to prove one's identity, but it requires careful selection of relay servers. There's also no guarantee that a relay server will retain or delete your data. Being essentially decentralized, it seems it won't implement non-essential but convenient features (like visibility control for non-encrypted messages).

## Bluesky:
Bluesky is a project that has become active recently, so the details are still unclear, but it seems very similar to Concurrent. If Bluesky truly works, there might be no need for Concurrent.

## The problem of the Concurrent?:
Concurrent is built on the concept of "protect your own identity." On the other hand, realistically protecting oneself requires a certain level of knowledge and skill, meaning there is a degree of difficulty and complexity involved.

In that sense, for the average person, centralized social media platforms, where you rely on others to protect you while trying to stay in the good graces of the operators to avoid suspension, might feel safer.

# For geeks
## How to launch own server
look at detailed documentation: [concurrent square](https://square.concurrent.world/operator/basic/index.html)

## Contributing
When creating a PR, we generally recommend creating an issue first and reaching a consensus on whether or not to proceed. (Concurrent is currently being heavily developed, and there may be changes that cannot be made due to its policy.)
