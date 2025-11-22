# Concrnt Protocol Overview

A quick reference guide to the Concrnt protocol.

## What is Concrnt?

Concrnt is a distributed social media protocol that enables:
- **Portable Identity**: Your account is tied to a cryptographic key, not a server
- **Censorship Resistance**: Migrate to any server while keeping your identity and content
- **Federated Communities**: Join and create community timelines accessible across servers
- **User-Controlled Security**: You manage your own keys and security

## Key Concepts

### Identities (CCIDs)

```
con1abc123def456... (42 chars)
```

Your identity is a Bech32-encoded address derived from your secp256k1 private key. Like a blockchain address, it proves you control the account.

### Documents

Everything in Concrnt is a signed JSON document:

```json
{
  "signer": "con1abc...",
  "type": "message",
  "body": { "body": "Hello, Concrnt!" },
  "signedAt": "2025-01-01T12:00:00Z"
}
```

Documents are signed with ECDSA (secp256k1) and verified cryptographically.

### Timelines

Timelines are ordered streams of content (like Twitter feeds or subreddits). Key differences:
- Created by users, not servers
- Accessible across all federated servers
- Can be public or private (via policies)

### Federation

Servers communicate via HTTP/WebSocket:
- **Pull**: Fetch content when needed
- **Push**: Real-time updates via WebSocket
- **Cache**: Store remote content locally

## Core Document Types

| Type | Purpose | Mutable | Example |
|------|---------|---------|---------|
| **Message** | Posts, media | No | A tweet, photo, video |
| **Association** | Reactions, replies | No | Like, reply, repost |
| **Profile** | User metadata | Yes | Username, bio, avatar |
| **Timeline** | Content streams | Yes | Community feed, topic |
| **Subscription** | Timeline collections | Yes | Personal feed aggregator |
| **Affiliation** | Server membership | No | Join a server |
| **Key** | Subkey delegation | No | Device key, app key |

## Cryptography

- **Signature Algorithm**: ECDSA on secp256k1 (same as Ethereum)
- **Hash Function**: Keccak256 (SHA-3 variant)
- **Address Format**: Bech32 with HRP "con" (users) or "ccs" (servers)
- **Signature Format**: 65-byte signature encoded as 130-char hex

## Identifiers

### CCID (Concrnt Canonical ID)
User/server identity: `con1abc...` (42 chars)

### CSID (Concrnt Server ID)
Server signing key: `ccs1abc...` (42 chars)

### CDID (Concrnt Document ID)
Time-sortable document ID: `C1234567890abcdefghijk1234` (27 chars with prefix)

Prefixes:
- `C` = Message
- `A` = Association  
- `P` = Profile
- `T` = Timeline
- `S` = Subscription

## Protocol Flow

### 1. User Registration

```
1. Generate key pair (secp256k1)
2. Derive CCID from public key
3. Affiliate with a server
4. Create profile
```

### 2. Creating a Post

```
1. Create message document
2. Sign with private key
3. Submit to server
4. Server validates and stores
5. Server broadcasts to subscribers
```

### 3. Cross-Server Federation

```
Server A                    Server B
    |                          |
    | Fetch entity con1user... |
    |------------------------->|
    |                          |
    | Return entity + proof    |
    |<-------------------------|
    |                          |
    | Subscribe to timeline    |
    |------------------------->|
    |                          |
    | Real-time events         |
    |<-------------------------|
```

### 4. Account Migration

```
1. Affiliate with new server (New Server)
2. Sign affiliation document with same CCID
3. Announce migration to followers
4. New server verifies ownership via signature
5. Followers update federation links
```

Your CCID stays the same, proving continuity.

## API Examples

### Get Entity
```bash
GET https://example.com/api/v1/entities/con1abc...
```

### Get Timeline Items
```bash
GET https://example.com/api/v1/timelines/T1234.../items?limit=50
```

### Commit Document
```bash
POST https://example.com/api/v1/commit
{
  "document": "{...}",
  "signature": "abc123...",
  "mode": "local"
}
```

### WebSocket Subscription
```javascript
ws = new WebSocket('wss://example.com/api/v1/timelines/socket')
ws.send(JSON.stringify({
  subscribe: ['T1234...@example.com']
}))
```

## Security Model

### User Responsibilities
- ✓ Keep private key secure
- ✓ Back up keys
- ✓ Manage key rotation
- ✓ Verify signatures

### Server Responsibilities
- ✓ Validate signatures
- ✓ Enforce policies
- ✓ Rate limiting
- ✓ Content storage

### Trust Model
- **Don't trust servers** with identity - you control the private key
- **Do trust servers** for availability and content delivery
- **Verify everything** - all content is cryptographically signed

## Comparison Summary

| Feature | Concrnt | Nostr | ActivityPub | AT Protocol |
|---------|---------|-------|-------------|-------------|
| Identity Control | User (key) | User (key) | Server | User (DID) |
| Migration | Always possible | N/A | Needs cooperation | Portable |
| Federation | Pull/Push hybrid | Relay | Push | PDS + Index |
| Scale Target | Communities | Personal | Medium | Global |
| Complexity | Medium | Low | Medium | High |
| Content Storage | Server committed | Relay optional | Server | PDS + CDN |

## Architectural Principles

1. **Cryptographic Identity First**: Everything derives from key ownership
2. **Community-Centric**: Optimize for moderate-sized, topic-based communities
3. **Federation-Friendly**: Lightweight server-to-server communication
4. **User Responsibility**: Users manage security, servers provide availability
5. **Immutable Core**: Documents are immutable and verifiable

## Getting Started

### For Users
1. Generate a key pair (use client app)
2. Join a server (affiliation)
3. Create a profile
4. Find or create timelines
5. Post content

### For Server Operators
1. Install Concrnt server
2. Generate server keys (CCID + CSID)
3. Configure domain and policies
4. Open for registrations
5. Federate with other servers

### For Developers
1. Review full specification: [`SPECIFICATION.md`](./SPECIFICATION.md)
2. Implement signing/verification
3. Use REST API for content
4. WebSocket for real-time updates
5. Follow schemas for interoperability

## Learn More

- **Full Specification**: See [`SPECIFICATION.md`](./SPECIFICATION.md) for complete technical details
- **Repository**: https://github.com/concrnt/concrnt
- **Documentation**: https://square.concrnt.net
- **Try It**: https://concrnt.world

## FAQ

**Q: What if I lose my private key?**  
A: You lose access to your identity. Backup is critical. Consider using a hardware wallet or secure key management system.

**Q: Can I have multiple accounts?**  
A: Yes, generate multiple key pairs. Each CCID is a separate identity.

**Q: What if my server goes down?**  
A: Migrate to another server using the same CCID. Your identity and signed content persist.

**Q: Is encryption supported?**  
A: Not at protocol level. Concrnt is designed for public content. Use end-to-end encryption at application layer if needed.

**Q: How does it prevent spam?**  
A: Servers use policies, rate limiting, and reputation systems. Users can block entities/domains.

**Q: Can I use it anonymously?**  
A: You can create pseudonymous identities (generate key without linking to real identity), but all actions are tied to your CCID.

---

**Next Steps**: Read the [full specification](./SPECIFICATION.md) for implementation details.
