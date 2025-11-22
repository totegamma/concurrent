# Concrnt Protocol Specification

Version: 1.0 (Draft)  
Last Updated: 2025-11-22

## Abstract

Concrnt is a distributed social media protocol designed to provide censorship-resistant, portable digital identities with cryptographic authentication. Unlike centralized platforms where account suspension means permanent loss, Concrnt enables users to migrate their identity, posts, and social connections between servers while maintaining cryptographic proof of ownership.

This specification describes the Concrnt protocol architecture, data structures, cryptographic primitives, and federation mechanisms.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Protocol Overview](#2-protocol-overview)
3. [Core Concepts](#3-core-concepts)
4. [Cryptographic Foundations](#4-cryptographic-foundations)
5. [Identity System](#5-identity-system)
6. [Document Types](#6-document-types)
7. [Timeline and Stream Architecture](#7-timeline-and-stream-architecture)
8. [Federation Protocol](#8-federation-protocol)
9. [Policy System](#9-policy-system)
10. [WebSocket Real-time Protocol](#10-websocket-real-time-protocol)
11. [Security Considerations](#11-security-considerations)
12. [Comparison with Other Protocols](#12-comparison-with-other-protocols)

---

## 1. Introduction

### 1.1 Motivation

Social media accounts have become integral to digital identity. However, centralized platforms pose risks:
- Irreversible account suspensions
- Third-party censorship
- Loss of cultivated relationships and content
- Lack of user control over their data

Decentralized protocols like ActivityPub (Mastodon, Misskey) offer some improvements but still face limitations:
- Account migration requires server cooperation
- If suspended before migration, recovery is impossible
- Server-local timelines fragment communities

### 1.2 Design Goals

Concrnt addresses these issues through:

1. **Cryptographic Identity**: Identity is proven through public-key cryptography, not server authority
2. **Account Portability**: Users can migrate to any server while maintaining their identity and content
3. **Decentralized Federation**: Servers cooperate through a peer-to-peer model
4. **Community-First Architecture**: Federated timelines instead of server-local isolation
5. **User-Controlled Security**: Users manage their own keys and security

### 1.3 Trade-offs

Concrnt optimizes for:
- Community-based interaction over global scale
- User responsibility over convenience
- Decentralization over performance

Concrnt is NOT designed for:
- Twitter-scale infinite following
- Users unwilling to manage cryptographic keys
- Single monolithic index servers

---

## 2. Protocol Overview

### 2.1 Architecture

```
┌──────────────────────────────────────────────────────────┐
│                     Application Layer                     │
│  (Web Client, Mobile Client, CLI Tools)                  │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│                      API Layer (REST)                     │
│  /api/v1/entities, /api/v1/messages, /api/v1/timelines  │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│                  Federation Layer (HTTP/WS)               │
│     Server-to-Server communication via signed documents   │
└──────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────┐
│                    Core Protocol Layer                    │
│   Document Validation, Signature Verification, Storage   │
└──────────────────────────────────────────────────────────┘
```

### 2.2 Key Components

- **Entities**: Cryptographically verified identities (users, bots)
- **Domains**: Servers hosting entities and content
- **Messages**: Content items (posts, media)
- **Associations**: Relationships between entities and messages (likes, replies)
- **Timelines**: Ordered streams of content
- **Subscriptions**: User-curated collections of timelines
- **Keys**: Hierarchical key system for identity delegation

---

## 3. Core Concepts

### 3.1 Identifiers

#### 3.1.1 CCID (Concrnt Canonical ID)

CCIDs are cryptographic addresses derived from secp256k1 public keys using Bech32 encoding with the HRP (Human Readable Part) `con`.

Format: `con1<base32-encoded-address>`

Example: `con1abc123def456...` (42 characters)

CCIDs uniquely identify entities across the network and are derived from the user's private key, ensuring cryptographic ownership.

#### 3.1.2 CSID (Concrnt Server ID)

Similar to CCIDs but used for server identification with HRP `ccs`.

Format: `ccs1<base32-encoded-address>` (42 characters)

Each domain has a CSID derived from the server's private key, used for server-to-server authentication.

#### 3.1.3 CDID (Concrnt Document ID)

CDIDs are sortable, time-embedded identifiers for documents and resources.

Structure:
- 10 bytes: Random data
- 6 bytes: Timestamp (milliseconds since epoch)
- Encoding: Base32 using custom alphabet `0123456789abcdefghjkmnpqrstvwxyz` (excluding i, l, o, u)

Format: 26 characters without prefix, or 27 characters with type prefix

Prefixes:
- `C`: Message
- `A`: Association
- `P`: Profile
- `T`: Timeline
- `S`: Subscription

Example: `C1234567890abcdefghijk1234`

### 3.2 Domains

A domain represents a Concrnt server instance identified by:
- **FQDN**: Fully qualified domain name (e.g., `concrnt.world`)
- **CCID**: Server's identity address
- **CSID**: Server's signing address

Domains host:
- Entity affiliations (user accounts on that server)
- Local timelines
- Federated content cache

### 3.3 Federation Model

Concrnt uses a hybrid pull/push federation model:

1. **Pull**: Servers fetch content from other servers when needed
2. **Push**: Servers send updates via WebSocket for real-time subscriptions
3. **Caching**: Servers cache remote content for performance

Unlike ActivityPub's follower-push model, Concrnt's architecture allows:
- Reading public content without prior subscription
- Lightweight server-to-server communication
- Real-time timeline synchronization

---

## 4. Cryptographic Foundations

### 4.1 Signature Algorithm

Concrnt uses **secp256k1** elliptic curve cryptography (same as Ethereum and Bitcoin).

**Hash Function**: Keccak256 (SHA-3 variant)

**Signature Format**: 65-byte ECDSA signature (r, s, v) encoded as 130-character hexadecimal

### 4.2 Signing Process

```
1. Serialize document to JSON string
2. Hash = Keccak256(document_bytes)
3. Signature = ECDSA_Sign(Hash, private_key)
4. Encode signature as hex string
```

### 4.3 Verification Process

```
1. Parse signature from hex
2. Hash = Keccak256(document_bytes)
3. Recover public key from signature
4. Compress public key
5. Derive Bech32 address
6. Compare with expected address
```

### 4.4 Address Derivation

```
1. Generate secp256k1 key pair
2. Extract public key (33 bytes compressed)
3. Hash public key with SHA256
4. Take RIPEMD160 of result
5. Encode with Bech32 using HRP "con" or "ccs"
```

This scheme is compatible with Cosmos SDK address formats.

---

## 5. Identity System

### 5.1 Entity Lifecycle

#### 5.1.1 Creation

An entity is created when a user generates a key pair and affiliates with a domain:

```json
{
  "signer": "con1abc...",
  "owner": "con1abc...",
  "type": "affiliation",
  "domain": "example.com",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

#### 5.1.2 Affiliation

Affiliation binds an entity to a domain. Users can be affiliated with multiple domains simultaneously.

**Affiliation Document**:
```json
{
  "signer": "con1useraddress...",
  "owner": "con1useraddress...",
  "type": "affiliation",
  "domain": "example.com",
  "signedAt": "2025-01-01T00:00:00Z",
  "meta": {
    "username": "alice"
  }
}
```

#### 5.1.3 Migration

To migrate:
1. Affiliate with new domain
2. Announce migration to followers
3. Old domain marks account as migrated
4. New domain verifies ownership via cryptographic signature

The CCID remains constant, proving continuity of identity.

#### 5.1.4 Tombstone

A tombstone marks an entity as inactive/deleted:

```json
{
  "signer": "con1useraddress...",
  "type": "tombstone",
  "reason": "Account deactivated by user",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

### 5.2 Hierarchical Key System

Concrnt supports key delegation through a hierarchical system:

```
Root Key (CCID)
    ├─ Subkey 1
    │   └─ Subkey 1.1
    └─ Subkey 2
```

**Benefits**:
- Use different keys for different devices
- Revoke compromised keys without losing identity
- Grant limited permissions to third-party apps

#### 5.2.1 Key Enactment

```json
{
  "type": "enact",
  "signer": "con1parent...",
  "keyID": "con1parent...",
  "target": "con1newkey...",
  "parent": "con1parent...",
  "root": "con1rootkey...",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

#### 5.2.2 Key Revocation

```json
{
  "type": "revoke",
  "signer": "con1parent...",
  "keyID": "con1parent...",
  "target": "con1revokedkey...",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

#### 5.2.3 Key Resolution

When verifying a document signed by a subkey:

1. Retrieve key chain from subkey to root
2. Verify each enactment signature
3. Check no key in chain is revoked
4. Verify root key matches expected CCID

### 5.3 Authentication

#### 5.3.1 Passport

A passport bundles entity information for cross-domain authentication:

```json
{
  "document": "{...}",
  "signature": "abc123..."
}
```

Passport Document:
```json
{
  "type": "passport",
  "signer": "ccs1server...",
  "domain": "example.com",
  "entity": {
    "ccid": "con1user...",
    "affiliationDocument": "{...}",
    "affiliationSignature": "..."
  },
  "keys": [
    {
      "id": "con1key...",
      "enactDocument": "{...}",
      "enactSignature": "..."
    }
  ],
  "signedAt": "2025-01-01T00:00:00Z"
}
```

#### 5.3.2 JWT Authentication

Concrnt uses custom JWTs with algorithm "CONCRNT":

**Header**:
```json
{
  "typ": "JWT",
  "alg": "CONCRNT",
  "kid": "con1key..."
}
```

**Claims**:
```json
{
  "iss": "con1user...",
  "sub": "CONCRNT_AUTH",
  "aud": "https://example.com",
  "exp": "1735689600"
}
```

**Signature**: ECDSA signature of `base64(header).base64(claims)`

Verification requires:
1. Verify JWT signature
2. Check expiration
3. Validate passport if using subkey
4. Verify affiliation is current

---

## 6. Document Types

All documents follow a common structure:

```json
{
  "id": "optional-document-id",
  "signer": "con1address...",
  "owner": "con1address...",
  "type": "document-type",
  "schema": "https://schema-url",
  "policy": "https://policy-url",
  "policyParams": "{...}",
  "keyID": "con1key...",
  "body": { ... },
  "meta": { ... },
  "semanticID": "custom-id",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

### 6.1 Message Document

Messages are immutable content items (posts, media, etc.).

```json
{
  "id": "C1234567890abcdefghijk1234",
  "signer": "con1user...",
  "owner": "con1user...",
  "type": "message",
  "schema": "https://schema.concrnt.net/m/post.json",
  "timelines": ["T1234...", "T5678..."],
  "body": {
    "body": "Hello, Concrnt!",
    "mentions": [],
    "attachments": []
  },
  "signedAt": "2025-01-01T12:00:00Z"
}
```

**Fields**:
- `id`: CDID with prefix 'C'
- `schema`: Defines message structure (posts, images, videos, etc.)
- `timelines`: List of timeline IDs where message appears
- `body`: Schema-specific content
- `policy`: Access control policy URL

### 6.2 Association Document

Associations are relationships between entities and messages (likes, replies, reposts).

```json
{
  "id": "A1234567890abcdefghijk1234",
  "signer": "con1user...",
  "owner": "con1user...",
  "type": "association",
  "schema": "https://schema.concrnt.net/a/like.json",
  "target": "C1234567890abcdefghijk1234",
  "variant": "👍",
  "timelines": ["T1234..."],
  "body": {},
  "signedAt": "2025-01-01T12:01:00Z"
}
```

**Fields**:
- `id`: CDID with prefix 'A'
- `target`: ID of the message being associated with
- `variant`: Subtype (e.g., emoji for reactions)
- `schema`: Type of association (like, reply, repost)

Common schemas:
- `like.json`: Simple like/favorite
- `reply.json`: Reply to a message
- `repost.json`: Share/repost

### 6.3 Profile Document

Profiles are mutable user metadata (display name, bio, avatar).

```json
{
  "id": "P1234567890abcdefghijk1234",
  "signer": "con1user...",
  "type": "profile",
  "schema": "https://schema.concrnt.net/p/userprofile.json",
  "semanticID": "profile",
  "body": {
    "username": "Alice",
    "description": "Software developer",
    "avatar": "https://example.com/avatar.jpg",
    "banner": "https://example.com/banner.jpg"
  },
  "signedAt": "2025-01-01T00:00:00Z"
}
```

Profiles are mutable - newer documents with same `semanticID` replace older ones.

### 6.4 Timeline Document

Timelines are ordered streams of content.

```json
{
  "id": "T1234567890abcdefghijk1234",
  "signer": "con1user...",
  "owner": "con1user...",
  "author": "con1user...",
  "type": "timeline",
  "schema": "https://schema.concrnt.net/t/community.json",
  "indexable": true,
  "domainOwned": false,
  "body": {
    "name": "Tech Discussion",
    "description": "A place to discuss technology"
  },
  "policy": "https://example.com/timeline-policy.json",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

**Fields**:
- `indexable`: Whether timeline appears in public listings
- `domainOwned`: Whether timeline is managed by domain (vs user)
- `policy`: Who can post to timeline

### 6.5 Subscription Document

Subscriptions are user-curated collections of timelines.

```json
{
  "id": "S1234567890abcdefghijk1234",
  "signer": "con1user...",
  "owner": "con1user...",
  "type": "subscription",
  "schema": "https://schema.concrnt.net/s/subscription.json",
  "indexable": false,
  "body": {
    "name": "My Feed",
    "description": "My personal feed"
  },
  "signedAt": "2025-01-01T00:00:00Z"
}
```

### 6.6 Subscribe/Unsubscribe Document

```json
{
  "signer": "con1user...",
  "type": "subscribe",
  "subscription": "S1234567890abcdefghijk1234",
  "target": "T9876543210zyxwvutsrqpo9876",
  "body": {},
  "signedAt": "2025-01-01T00:00:00Z"
}
```

### 6.7 Delete Document

Marks a message for deletion.

```json
{
  "signer": "con1user...",
  "type": "delete",
  "target": "C1234567890abcdefghijk1234",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

Deletion is a soft delete - original document may still exist in archives.

### 6.8 Retract Document

Removes a message from a specific timeline.

```json
{
  "signer": "con1user...",
  "type": "retract",
  "timeline": "T1234567890abcdefghijk1234",
  "target": "C1234567890abcdefghijk1234",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

### 6.9 Ack/Unack Document

Acknowledges/follows another entity.

```json
{
  "signer": "con1user...",
  "type": "ack",
  "from": "con1user...",
  "to": "con1friend...",
  "signedAt": "2025-01-01T00:00:00Z"
}
```

---

## 7. Timeline and Stream Architecture

### 7.1 Timeline Structure

Timelines are append-only logs of content.

**Timeline Item**:
```json
{
  "resourceID": "C1234567890abcdefghijk1234",
  "timelineID": "T1234567890abcdefghijk1234",
  "owner": "con1user...",
  "author": "con1creator...",
  "schema": "https://schema.concrnt.net/m/post.json",
  "document": "{...}",
  "signature": "abc123...",
  "cdate": "2025-01-01T12:00:00Z"
}
```

### 7.2 Chunking

For efficient synchronization, timelines are divided into chunks:

**Chunk**:
```json
{
  "key": "T1234567890abcdefghijk1234@example.com",
  "epoch": "2025-01-01T00:00:00Z",
  "items": [
    { "resourceID": "C1234...", ... },
    { "resourceID": "C5678...", ... }
  ]
}
```

Chunks represent time windows (e.g., hourly, daily) for efficient range queries.

### 7.3 Real-time Subscriptions

Clients subscribe to timelines via WebSocket:

```json
{
  "subscribe": [
    "T1234567890abcdefghijk1234@example.com",
    "T5678901234abcdefghijk5678@other.com"
  ]
}
```

Server sends events:
```json
{
  "timeline": "T1234567890abcdefghijk1234@example.com",
  "item": { ... },
  "resource": { ... },
  "document": "{...}",
  "signature": "abc123..."
}
```

---

## 8. Federation Protocol

### 8.1 Server Discovery

Servers are discovered via HTTPS at well-known endpoints:

```
GET https://example.com/.well-known/concrnt
```

Response:
```json
{
  "name": "Example Server",
  "version": "1.0",
  "ccid": "con1server...",
  "csid": "ccs1server..."
}
```

### 8.2 Entity Resolution

To resolve an entity from another server:

```
GET https://example.com/api/v1/entities/con1user...
```

Response:
```json
{
  "ccid": "con1user...",
  "domain": "example.com",
  "affiliationDocument": "{...}",
  "affiliationSignature": "...",
  "profiles": [...]
}
```

### 8.3 Content Federation

#### 8.3.1 Fetching Messages

```
GET https://example.com/api/v1/messages/C1234567890abcdefghijk1234
```

#### 8.3.2 Fetching Timeline Items

```
GET https://example.com/api/v1/timelines/T1234567890abcdefghijk1234/items?until=2025-01-01T12:00:00Z&limit=50
```

#### 8.3.3 Fetching Chunks

```
POST https://example.com/api/v1/timelines/chunks
{
  "T1234...@example.com": "itr_abc123",
  "T5678...@other.com": "itr_def456"
}
```

### 8.4 Real-time Federation

Servers establish WebSocket connections for real-time updates:

```
WS wss://example.com/api/v1/timelines/socket
```

Subscribe to remote timelines:
```json
{
  "subscribe": ["T1234...@example.com"]
}
```

Receive events:
```json
{
  "timeline": "T1234...@example.com",
  "item": { ... },
  "document": "{...}",
  "signature": "..."
}
```

### 8.5 Commit Protocol

When creating content, clients commit to their home server:

```
POST https://home.example.com/api/v1/commit
{
  "document": "{...}",
  "signature": "...",
  "mode": "local"
}
```

**Commit Modes**:
- `local`: Store on home server only
- `federate`: Propagate to federated servers immediately
- `defer`: Queue for later propagation

The server validates:
1. Document structure
2. Signature authenticity
3. Key validity
4. Policy compliance
5. Schema conformance

---

## 9. Policy System

### 9.1 Policy Document Structure

```json
{
  "name": "Timeline Post Policy",
  "description": "Controls who can post to this timeline",
  "versions": {
    "1": {
      "statements": {
        "canPost": {
          "dominant": false,
          "defaultOnTrue": true,
          "defaultOnFalse": false,
          "condition": {
            "op": "or",
            "args": [
              {
                "op": "eq",
                "args": [
                  { "const": { "$": "requester.id" } },
                  { "const": { "$": "self.owner" } }
                ]
              },
              {
                "op": "in",
                "args": [
                  { "const": { "$": "requester.domain" } },
                  { "const": ["example.com", "trusted.com"] }
                ]
              }
            ]
          }
        }
      },
      "defaults": {
        "canPost": false
      }
    }
  }
}
```

### 9.2 Policy Evaluation

Policies are evaluated in the context of a request:

```json
{
  "requester": { "id": "con1user...", "domain": "example.com" },
  "requesterDomain": { "fqdn": "example.com", "ccid": "con1server..." },
  "document": { ... },
  "self": { ... },
  "resource": { ... },
  "params": { ... }
}
```

### 9.3 Policy Operators

- Comparison: `eq`, `ne`, `lt`, `le`, `gt`, `ge`
- Logical: `and`, `or`, `not`
- Membership: `in`, `contains`
- Access: `get` (access nested properties)

---

## 10. WebSocket Real-time Protocol

### 10.1 Connection Establishment

```
WS wss://example.com/api/v1/timelines/socket?token=jwt_token
```

### 10.2 Subscription Management

**Subscribe**:
```json
{
  "subscribe": [
    "T1234567890abcdefghijk1234@example.com",
    "T5678901234abcdefghijk5678@example.com"
  ]
}
```

**Unsubscribe**:
```json
{
  "unsubscribe": [
    "T1234567890abcdefghijk1234@example.com"
  ]
}
```

### 10.3 Event Delivery

```json
{
  "timeline": "T1234567890abcdefghijk1234@example.com",
  "item": {
    "resourceID": "C1234567890abcdefghijk1234",
    "timelineID": "T1234567890abcdefghijk1234",
    "owner": "con1user...",
    "cdate": "2025-01-01T12:00:00Z"
  },
  "resource": {
    "id": "C1234567890abcdefghijk1234",
    "author": "con1user...",
    "schema": "https://schema.concrnt.net/m/post.json",
    "body": { ... }
  },
  "document": "{...}",
  "signature": "abc123..."
}
```

### 10.4 Error Handling

```json
{
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Invalid authentication token"
  }
}
```

---

## 11. Security Considerations

### 11.1 Key Management

- **Private keys must never be transmitted**: All operations requiring private keys are performed client-side
- **Key rotation**: Use hierarchical keys to enable rotation without changing CCID
- **Revocation**: Compromised keys can be revoked immediately
- **Backup**: Users are responsible for backing up private keys

### 11.2 Signature Verification

- All documents must be cryptographically signed
- Servers must verify signatures before accepting content
- Reject documents with invalid or missing signatures
- Verify key chains for subkeys

### 11.3 Replay Attacks

- Documents include `signedAt` timestamp
- Servers should reject documents with timestamps too far in the past or future
- For critical operations, use nonces or challenge-response

### 11.4 Denial of Service

- Rate limiting per entity/domain
- Resource quotas (message size, association count)
- Policy-based access control
- Server operators can block abusive entities/domains

### 11.5 Content Integrity

- Documents are immutable once signed
- Deletion is soft - original documents may persist
- Servers should periodically verify stored signatures
- Content addressing via CDIDs ensures integrity

### 11.6 Privacy

- **Public by default**: Concrnt emphasizes public, federated content
- **Encryption**: Not built into protocol - use end-to-end encryption at application layer
- **Metadata**: All metadata (timestamps, associations) is visible
- **Visibility control**: Use policies to restrict access, but servers can ignore these

### 11.7 Spam and Abuse

- Policies control who can post to timelines
- Servers can filter content based on entity/domain reputation
- Users can block entities/domains client-side
- Distributed moderation - each server sets own rules

---

## 12. Comparison with Other Protocols

### 12.1 vs. Nostr

**Similarities**:
- Cryptographic identity (private key ownership)
- Relay-based distribution
- User controls their identity

**Differences**:
- Concrnt has stronger server accountability (affiliations)
- Concrnt emphasizes timelines over individual notes
- Nostr is simpler and more decentralized
- Concrnt has built-in policy system for access control
- Nostr relays have no content guarantees; Concrnt servers commit to storage

**Trade-off**: Concrnt provides more features but requires more server infrastructure.

### 12.2 vs. ActivityPub (Mastodon, Misskey)

**Similarities**:
- Federation between servers
- Social media features (posts, likes, follows)
- Distributed architecture

**Differences**:
- ActivityPub: Server owns identity; Concrnt: User owns identity cryptographically
- ActivityPub: Account migration requires server cooperation; Concrnt: Always possible with private key
- ActivityPub: Follower-push model; Concrnt: Pull/push hybrid
- ActivityPub: Server-local timelines; Concrnt: Federated community timelines
- ActivityPub: JSON-LD; Concrnt: Simple JSON with schemas

**Trade-off**: Concrnt offers better censorship resistance but requires users to manage keys.

### 12.3 vs. AT Protocol (Bluesky)

**Similarities**:
- Decentralized identity
- Account portability
- Cryptographic verification
- Federation

**Differences**:
- AT Protocol: Personal Data Server (PDS) + massive index; Concrnt: Lightweight servers
- AT Protocol: Global view emphasis; Concrnt: Community-first
- AT Protocol: DID-based; Concrnt: secp256k1 addresses
- AT Protocol: Content-addressed storage (IPFS-like); Concrnt: Server-stored with CDIDs

**Trade-off**: AT Protocol scales to global feeds; Concrnt optimizes for moderate-sized communities.

---

## Appendices

### Appendix A: Schema URLs

Standard schemas are published at `https://schema.concrnt.net/`:

- Messages: `/m/post.json`, `/m/image.json`, `/m/video.json`
- Associations: `/a/like.json`, `/a/reply.json`, `/a/repost.json`
- Profiles: `/p/userprofile.json`
- Timelines: `/t/community.json`, `/t/personal.json`

### Appendix B: Error Codes

- `INVALID_SIGNATURE`: Signature verification failed
- `INVALID_DOCUMENT`: Document structure invalid
- `KEY_REVOKED`: Signing key has been revoked
- `UNAUTHORIZED`: Insufficient permissions
- `RATE_LIMITED`: Too many requests
- `NOT_FOUND`: Resource does not exist
- `POLICY_VIOLATION`: Action blocked by policy

### Appendix C: API Endpoints

**Core Entities**:
- `GET /api/v1/entities/:ccid` - Get entity information
- `POST /api/v1/commit` - Commit a document

**Messages**:
- `GET /api/v1/messages/:id` - Get message by ID
- `GET /api/v1/messages/:id/associations` - Get associations for message

**Timelines**:
- `GET /api/v1/timelines/:id` - Get timeline metadata
- `GET /api/v1/timelines/:id/items` - Get timeline items
- `POST /api/v1/timelines/chunks` - Batch fetch chunks

**Subscriptions**:
- `GET /api/v1/subscriptions/:id` - Get subscription metadata
- `GET /api/v1/subscriptions/:id/items` - Get aggregated items

**Real-time**:
- `WS /api/v1/timelines/socket` - Real-time timeline updates

### Appendix D: References

- secp256k1: https://en.bitcoin.it/wiki/Secp256k1
- Keccak256: https://keccak.team/keccak.html
- Bech32: https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki
- Cosmos SDK: https://docs.cosmos.network/

---

## Revision History

- 2025-11-22: Initial draft v1.0

## License

This specification is released into the public domain or under CC0, whichever is applicable in your jurisdiction.

## Contact

For questions, suggestions, or contributions:
- Repository: https://github.com/concrnt/concrnt
- Website: https://concrnt.world
