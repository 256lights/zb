# zb store RPC protocol

The `zb` command-line interface communicates with
a zb server (referred to as a *store*, since each server manages a collection of store objects)
using [JSON-RPC 2.0][] over a Unix socket.
These RPCs allow the `zb` command-line interface to inspect existing store objects,
add new store objects,
and initiate realization of derivation (`.drv`) files.

While the RPCs are primarily designed for use with the `zb` command-line interface,
the authors expect that other developer tooling may wish to integrate directly with a zb store.
Or perhaps alternate server implementations may exist over time
to provide different storage mechanisms or orchestrate builds.
This document aims to provide a description of the RPC protocol
for developers who are interested in creating such tools.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in [RFC 2119][].

[JSON-RPC 2.0]: https://www.jsonrpc.org/specification
[RFC 2119]: https://datatracker.ietf.org/doc/html/rfc2119

## Transport

Although JSON-RPC is a well-defined standard, it is intentionally transport-agnostic.
`zb` uses a framing mechanism similar to the [Language Server Protocol][]
to transmit and receive RPCs as well as large binary payloads.
In zb, the store is the server and the remote end is the client.

### Message format

Given a bidirectional stream of bytes (e.g. a TCP socket),
zb splits each direction of the stream into messages with one or more [HTTP-style header fields][].
Each message **MUST** start with headers.
Each header field is comprised of a name and a value and **MUST** be separated by ": " (a colon followed by a space).
Each header field **MUST** be terminated by "\r\n" (a carriage return character followed by a newline character).
The message headers and message body **MUST** be separated by an additional "\r\n".
Clients and servers **MUST** provide a `Content-Type` header for each message.
If the sender knows the size of a message in advance,
the sender **SHOULD** provide a `Content-Length` header.
Clients or servers **MAY** refuse to process a message without a `Content-Length` header
by closing the connection.
If the `Content-Length` header is present,
then the n bytes following the "\r\n" separating the message header from the message body are the message body,
where n is the number from the header value.
If the client or server sends a message with a `Content-Length` header
but the remote peer does not support the `Content-Type`,
then the remote peer **SHOULD** ignore the message.

There are two `Content-Type` values defined by this document:

1. `application/zb-store-rpc+json` is used for JSON-RPC.
2. `application/zb-store-export` is used for transmitting store objects.

[Language Server Protocol]: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/
[HTTP-style header fields]: https://datatracker.ietf.org/doc/html/rfc7230#section-3.2

### `application/zb-store-rpc+json` content type

If the `Content-Type` header of a message is `application/zb-store-rpc+json`,
then the `Content-Length` header **MUST** be present.
The semantics of the body of such messages are defined in the [JSON-RPC 2.0 specification][JSON-RPC 2.0].

### `application/zb-store-export` content type

The `application/zb-store-export` type is used to send zb store objects.
The format is roughly the same as `nix-store --export`:
a sequence of zero or more [Nix Archive Format (NAR)][] files.
Each NAR file **MUST** be preceded by an eight-byte sequence:
one 0x01 byte followed by 7 zero bytes.
Each NAR file **MUST** be followed by the following fields
(referred to as the NAR file's *trailer*):

1. The eight-byte sequence: 0x4e, 0x49, 0x58, 0x45 ("NIXE"), followed by 4 zero bytes.

2. A NAR `str` production of the intended absolute store object path.

3. A NAR `int` production of the number of references this NAR contains.

4. n NAR `str` productions of the absolute store object paths of references this NAR contains,
   where n is the integer in the previous field.
   References must be in lexicographically ascending order.

5. A NAR `str` production of the absolute store object path of the `.drv` file that produced this store object.
   It is **RECOMMENDED** for this to be an empty string.

6. One of two options.
   The sender **SHOULD** precompute a content address for the store object
   to ensure its integrity during transmission
   and communicate its type.

    a. If the sender intends to send a content address,
       the sender **MUST** provide one 0x01 byte followed by 7 zero bytes,
       followed by a NAR `str` production of the store object's content address in text format.

    b. If the sender does not intend to send a content address,
       the sender **MUST** provide 8 zero bytes.
       In such a case, the receiver **MAY** assume that the store object is addressed as a "source" object,
       which implies hashing the NAR file,
       but the receiver **MUST** validate such an inferred content address against the store object path field.
       Otherwise, the receiver **SHOULD NOT** process the store object.

Finally, there **MUST** be 8 zero bytes present after the last NAR file's trailer
and these **MUST** be the last bytes in the message body.

[Nix Archive Format (NAR)]: https://nix.dev/manual/nix/2.22/protocols/nix-archive

## Methods

The JSON-RPC methods in the protocol are currently defined in [zbstorerpc.go][].
([#99][] tracks using an interface description language for specifying these RPCs.)

[#99]: https://github.com/256lights/zb/issues/99
[zbstorerpc.go]: zbstorerpc.go
