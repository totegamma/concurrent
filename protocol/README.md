# Concrnt Protocol Documentation

This directory contains the formal specification and documentation for the Concrnt protocol.

## Documents

### [SPECIFICATION.md](./SPECIFICATION.md)
The complete technical specification of the Concrnt protocol. This document describes:
- Protocol architecture and design goals
- Cryptographic foundations (secp256k1, Keccak256)
- Identity system and key management
- All document types and their structures
- Federation protocol and server-to-server communication
- Policy system for access control
- WebSocket real-time protocol
- Security considerations
- Comparison with other decentralized protocols (Nostr, ActivityPub, AT Protocol)

**Audience**: Protocol implementers, server operators, security researchers

### [OVERVIEW.md](./OVERVIEW.md)
A quick reference guide providing a high-level overview of Concrnt. This document includes:
- Key concepts and terminology
- Core document types summary
- Protocol flow diagrams
- API examples
- Comparison table with other protocols
- FAQ for common questions

**Audience**: Developers getting started, users wanting to understand the protocol, decision-makers evaluating Concrnt

## Quick Start

1. **New to Concrnt?** Start with [OVERVIEW.md](./OVERVIEW.md) for a high-level introduction
2. **Implementing a client or server?** Read [SPECIFICATION.md](./SPECIFICATION.md) for complete technical details
3. **Want to contribute?** Review both documents, then check the main [repository](https://github.com/concrnt/concrnt)

## Protocol Version

Current version: **1.0 (Draft)**

The Concrnt protocol is under active development. While the core cryptographic and document structures are stable, some details may evolve based on implementation experience and community feedback.

## Key Features

- **Cryptographic Identity**: Users control their identity through secp256k1 private keys
- **Account Portability**: Migrate between servers while maintaining identity and content
- **Federated Timelines**: Create and join community-based content streams across servers
- **Hierarchical Keys**: Support key rotation and delegation without changing identity
- **Policy-Based Access Control**: Flexible policies for content and timeline access
- **Real-time Federation**: WebSocket-based live updates across servers

## Protocol Principles

1. **User Sovereignty**: Users own their identity and control access to their private keys
2. **Verifiable Everything**: All content is cryptographically signed and verifiable
3. **Server Independence**: Identity exists independent of any server
4. **Community Focus**: Optimized for topic-based communities, not infinite-scale social graphs
5. **Federation-Friendly**: Lightweight server-to-server protocols

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Application Layer                      │
│            (Web/Mobile Clients, Bots)                   │
└─────────────────────────────────────────────────────────┘
                           ▲
                           │ REST/WebSocket API
                           ▼
┌─────────────────────────────────────────────────────────┐
│                    Concrnt Server                        │
│  ┌────────────┐  ┌──────────┐  ┌─────────────────┐    │
│  │ Validation │  │ Storage  │  │   Federation    │    │
│  │  & Policy  │  │  Engine  │  │ (HTTP/WS)       │    │
│  └────────────┘  └──────────┘  └─────────────────┘    │
└─────────────────────────────────────────────────────────┘
                           ▲
                           │ Federation Protocol
                           ▼
┌─────────────────────────────────────────────────────────┐
│                   Other Concrnt Servers                  │
└─────────────────────────────────────────────────────────┘
```

## Identifiers

- **CCID** (Concrnt Canonical ID): `con1...` (42 chars) - User/entity identity
- **CSID** (Concrnt Server ID): `ccs1...` (42 chars) - Server identity
- **CDID** (Concrnt Document ID): `C/A/P/T/S1234...` (27 chars) - Time-sortable document IDs

## Core Concepts

### Entities
Cryptographically-verified identities (users, bots, organizations). Each entity has a CCID derived from a secp256k1 key pair.

### Documents
Signed JSON objects representing all protocol actions (messages, associations, profiles, etc.). Every document includes:
- Signer (CCID)
- Type (message, association, profile, etc.)
- Signature (ECDSA)
- Timestamp

### Timelines
Ordered streams of content, similar to feeds or channels. Unlike traditional platforms:
- Created by users, not just servers
- Accessible across federated servers
- Controlled by policies (who can post, who can read)

### Federation
Servers communicate to share content and synchronize timelines:
- **Discovery**: Servers find each other via HTTPS
- **Content Fetching**: Pull content on-demand
- **Real-time Updates**: Push events via WebSocket
- **Caching**: Local storage of remote content

## Document Types

| Type | ID Prefix | Mutable | Description |
|------|-----------|---------|-------------|
| Message | C | No | Posts, media, content |
| Association | A | No | Likes, replies, reactions |
| Profile | P | Yes | User metadata (name, bio, avatar) |
| Timeline | T | Yes | Content streams |
| Subscription | S | Yes | User's feed collections |
| Affiliation | - | No | Server membership |
| Key | - | No | Hierarchical key delegation |

## Example: Creating a Message

```javascript
// 1. Create document
const document = {
  signer: "con1abc123def456...",
  owner: "con1abc123def456...",
  type: "message",
  schema: "https://schema.concrnt.net/m/post.json",
  timelines: ["T1234567890abcdefghijk1234"],
  body: {
    body: "Hello, Concrnt!",
    mentions: [],
    attachments: []
  },
  signedAt: "2025-01-01T12:00:00Z"
}

// 2. Sign document (client-side)
const signature = signWithPrivateKey(JSON.stringify(document), privateKey)

// 3. Submit to server
await fetch('https://example.com/api/v1/commit', {
  method: 'POST',
  body: JSON.stringify({
    document: JSON.stringify(document),
    signature: signature,
    mode: 'local'
  })
})
```

## Comparison with Other Protocols

### vs. Nostr
- ✓ Both use cryptographic identity
- ✓ Concrnt adds server accountability via affiliations
- ✓ Concrnt has richer timeline/community features
- ✗ Nostr is simpler and more minimalist

### vs. ActivityPub
- ✓ Concrnt: Identity independent of server
- ✓ Concrnt: Account migration always possible
- ✓ ActivityPub: More mature ecosystem
- ✗ ActivityPub: Server controls identity

### vs. AT Protocol (Bluesky)
- ✓ Both support portable identity
- ✓ AT Protocol: Designed for global scale
- ✓ Concrnt: Optimized for communities
- ≈ Different architectural approaches

## Implementation Status

- ✅ Core protocol implementation (Go)
- ✅ REST API server
- ✅ WebSocket real-time federation
- ✅ Policy engine
- ✅ Web client reference implementation
- 🚧 Mobile clients (in progress)
- 🚧 Additional language implementations

## Contributing

The Concrnt protocol is open for community input. To propose changes:

1. **Discuss**: Open an issue to discuss proposed changes
2. **Consensus**: Reach agreement on the direction
3. **Document**: Update specification documents
4. **Implement**: Develop reference implementation
5. **Review**: Community review and testing

## References

### Technical Standards
- secp256k1: https://en.bitcoin.it/wiki/Secp256k1
- Keccak256: https://keccak.team/
- Bech32: https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki
- WebSocket: https://datatracker.ietf.org/doc/html/rfc6455

### Related Protocols
- Nostr: https://github.com/nostr-protocol/nostr
- ActivityPub: https://www.w3.org/TR/activitypub/
- AT Protocol: https://atproto.com/

## License

The Concrnt protocol specification is released to the public domain (or under CC0 where applicable).

The reference implementation is licensed under the terms specified in the main repository.

## Links

- **Main Repository**: https://github.com/concrnt/concrnt
- **Documentation Site**: https://square.concrnt.net
- **Web Client**: https://concrnt.world
- **Community**: Join a Concrnt server to discuss

---

For questions or feedback, please open an issue in the main repository.
