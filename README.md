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
Concrnt uses public-key cryptography to verify user identities. It doesn't share any confidential information with servers, and users are responsible for managing the security of their own accounts.

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
Bluesky is a project that has become more active recently, so details are still emerging, but it seems to share many similarities with Concrnt. Bluesky appears to be created with a mission similar to Twitter's, aimed at "making all the information in the world shareable." In contrast, Concrnt's mission focuses on "centering around communities and loosely connecting with the world." This difference in mission leads Bluesky to adopt an architecture that builds a massive index server and generates feeds, whereas Concrnt uses a single server to lightly and in real-time collect information from nearby sources.

## The problem of the Concrnt?:
The architectural strategy adopted by Concrnt may not scale to an ultra-large system where countless users can follow as many others as they like, similar to Twitter. This is because, rather than constructing a single, massive home timeline, it is designed with the assumption that users will create and switch between several moderately sized lists.

This approach is because Concrnt does not primarily aim to intensely connect with the world but to build relationships based on community timelines, optimizing for this purpose. This allows each domain to control information independently without the need for a massive server like an index server.

Furthermore, Concrnt embraces the concept of "protecting one's identity by oneself." However, in reality, this requires a certain level of knowledge and expertise, meaning there is an inherent complexity and difficulty in use.

In this sense, centralized social networks, where one relies on others for protection while trying to stay in the good graces of the administration to avoid being frozen, might be more reassuring for the average person.

# For geeks
## How to launch own server
look at detailed documentation: [concrnt square](https://square.concrnt.net/operator/)

## Contributing
When creating a PR, we generally recommend creating an issue first and reaching a consensus on whether or not to proceed. (Concrnt is currently being heavily developed, and there may be changes that cannot be made due to its policy.)

# Upgrading Steps

### v1.3.3 CheckPoint
これ以下のバージョンから上のバージョンにアップデートする際、このバージョンを経由する必要があります

### v1.3.0 CheckPoint
これ以下のバージョンから上のバージョンにアップデートする際、このバージョンを経由する必要があります

### v1.2.1

### v1.2.0

### v1.1.3

### v1.1.1

### v1.1.0

### v1.0.13

### v1.0.12

### v1.0.9

### v1.0.3

