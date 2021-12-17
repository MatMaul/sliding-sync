# MSCXXXX: Client syncing with sliding windows (aka Sync v3)

This MSC outlines a replacement for the CS API endpoint `/sync`.

The current `/sync` endpoint scales badly as the number of rooms on an account increases. It scales
badly because all rooms are returned to the client, and clients cannot opt-out of a large amount of
extraneous data such as receipts. On large accounts with thousands of rooms, the initial sync
operation can take minutes to perform. This significantly delays the initial login to Matrix clients.

## Goals

Any improved `/sync` mechanism had a number of goals:
 - Sync time should be independent of the number of rooms you are in.
 - Time from launch to confident usability should be as low as possible.
 - Time from login on existing accounts to usability should be as low as possible.
 - Bandwidth should be minimised.
 - Support lazy-loading of things like read receipts (and avoid sending unnecessary data to the client)
 - Support informing the client when room state changes from under it, due to state resolution.
 - Clients should be able to work correctly without ever syncing in the full set of rooms they’re in.
 - Don’t incremental sync rooms you don’t care about.
 - Combining uploaded filters with ad-hoc filter parameters (which isn’t possible with sync v2 today)
 - Servers should not need to store all past since tokens. If a since token has been discarded we should gracefully degrade to initial sync.
 - Ability to filter by space.

These goals shaped the design of this proposal. 

## Proposal

At a high level, the proposal introduces a way for clients to filter and sort the rooms they are
joined to and then request a subset of the resulting list of rooms rather than the entire room list.
```
         All joined rooms on user's account
Q W E R T Y U I O P L K J H G F D S A Z X C V B N M
\                                                 /
 \                                               /
  \      Subset of rooms matched by filters     /
   Q W E R T Y U I O P L K J H G F D S A Z X C V
                       |
   A C D E F G H I J K L O P Q R S T U V W X Y Z     Rooms sorted by name (or by recency, etc)
   |_______|
       |

   A C D E F                                         first 5 rooms requested
```
It also introduces a number of new concepts which are explained in more detail later on:
 - Core API: The minimumal API to be sync v3 compatible.
 - Extensions: Additional APIs which expose more data from the server e.g presence, device messages.
 - Sticky Parameters: Clients can specify request parameters once and have the server remember what
   they were, without forcing the client to resend the parameter every time.

### Core
A complete sync request looks like:
`POST /v3/sync?pos=4`:
```js
{
  "lists": [
    {
      "rooms": [ [0,99] ],
      "sort": [ "by_notification_count", "by_recency", "by_name" ],
      "required_state": [
        ["m.room.join_rules", ""],
        ["m.room.history_visibility", ""],
        ["m.space.child", "*"]
      ],
      "timeline_limit": 10,
      "filters": {
        "is_dm": true
      }
    }
  ],
  "room_subscriptions": {
      "!sub1:bar": {
          "required_state": [ ["*","*"] ],
          "timeline_limit": 50
      }
  },
  "unsubscribe_rooms": [ "!sub3:bar" ]
  "extensions": {}
}
```
An entire response looks like:
`HTTP 200 OK`
```js
{
  "ops": [
    {
      "list": 0,
      "range": [0,99],
      "op": "SYNC",
      "rooms": [
        {
          "room_id": "!foo:bar",
          "name": "The calculated room name",
          "required_state": [
            {"sender":"@alice:example.com","type":"m.room.join_rules", "state_key":"", "content":{"join_rule":"invite"}},
            {"sender":"@alice:example.com","type":"m.room.history_visibility", "state_key":"", "content":{"history_visibility":"joined"}},
            {"sender":"@alice:example.com","type":"m.space.child", "state_key":"!foo:example.com", "content":{"via":["example.com"]}},
            {"sender":"@alice:example.com","type":"m.space.child", "state_key":"!bar:example.com", "content":{"via":["example.com"]}},
            {"sender":"@alice:example.com","type":"m.space.child", "state_key":"!baz:example.com", "content":{"via":["example.com"]}}
          ],
          "timeline": [
            {"sender":"@alice:example.com","type":"m.room.join_rules", "state_key":"", "content":{"join_rule":"invite"}},
            {"sender":"@alice:example.com","type":"m.room.message", "content":{"body":"A"}},
            {"sender":"@alice:example.com","type":"m.room.message", "content":{"body":"B"}},
            {"sender":"@alice:example.com","type":"m.room.message", "content":{"body":"C"}},
            {"sender":"@alice:example.com","type":"m.room.message", "content":{"body":"D"}},
          ],
          "notification_count": 54,
          "highlight_count": 3
        },
        // ... 99 more items
      ],
    }
  ],
  "room_subscriptions": {
    "!sub1:bar": {
        "name": "Alice and Bob",
        "required_state": [
        {"sender":"@alice:example.com","type":"m.room.create", "state_key":"", "content":{"creator":"@alice:example.com"}},
        {"sender":"@alice:example.com","type":"m.room.join_rules", "state_key":"", "content":{"join_rule":"invite"}},
        {"sender":"@alice:example.com","type":"m.room.history_visibility", "state_key":"", "content":{"history_visibility":"joined"}},
        {"sender":"@alice:example.com","type":"m.room.member", "state_key":"@alice:example.com", "content":{"membership":"join"}}
        ],
        "timeline": [
        {"sender":"@alice:example.com","type":"m.room.create", "state_key":"", "content":{"creator":"@alice:example.com"}},
        {"sender":"@alice:example.com","type":"m.room.join_rules", "state_key":"", "content":{"join_rule":"invite"}},
        {"sender":"@alice:example.com","type":"m.room.history_visibility", "state_key":"", "content":{"history_visibility":"joined"}},
        {"sender":"@alice:example.com","type":"m.room.member", "state_key":"@alice:example.com", "content":{"membership":"join"}}
        {"sender":"@alice:example.com","type":"m.room.message", "content":{"body":"A"}},
        {"sender":"@alice:example.com","type":"m.room.message", "content":{"body":"B"}},
        ],
        "notification_count": 1,
        "highlight_count": 0
    }
  }
  "counts": [1337], 
  "initial": true,
  "extensions": {}
}
```
These fields and their interactions are explained in the next few sections. This forms the core of
the API. Additional data can be returned via "extensions". 

#### Connections and streaming data

At a high level, the syncing mechanism creates a "connection" to the server to allow the
bi-directional exchange of JSON objects. This mechanism is ideally suited for WebSockets, which this
proposal will support (TODO), but more difficult to do for HTTP long-polling, which this proposal also
supports.

For the long-polling use case, this proposal includes an opaque token that is very similar to
`/sync` v2's `since` query parameter. This is called `pos` and represents the position in the stream
the client is currently at. Unlike `/sync` v2, this token is ephemeral and can be invalidated at any
time. When a client first connects to the server, no `pos` is specified. Also unlike `/sync` v2, this
token cannot be used with other APIs such as `/messages` or `/keys/changes`.

In simple servers, the `pos` may be an incrementing integer, but more complex servers may use vector
clocks or contain node identifying information in the token. Clients MUST treat `pos` as an opaque
value and not introspect it.

When a `pos` is invalidated, the server MUST treat the invalidated `pos` as if it was absent
(in other words that this is an initial sync) and set `initial: true` in the response to inform the
client that the response is now an initial sync. For clarity, `initial: true` MUST also be set when
there is no `pos` value provided. When there is a valid `pos`, this flag MUST be omitted (sending
`initial: false` is wasteful).

A response for a given `pos` must be idempotent to account for packet loss. For example:
```
Client                  Server 
  | ---------------------> |   data=[A,B], pos=2
  | <--data=[A,B], pos=2-- |
  |                        |   data=[C], pos=3  (new event arrives)
  | -----pos=2-----------> | 
  |   X--data=[C], pos=3-- |  Response is lost
  |                        |
  |                        | data=[C,D], pos=4  (another new event arrives)
  | -----pos=2-----------> | 
  | <----data=[C], pos=3-- | Server CANNOT send data=[C,D] pos=4, it MUST send the previous response
```

#### Sticky request parameters

Request parameters can be "sticky". This means that their value is remembered across multiple requests.
The lifetime of sticky request parameters are tied to a sync connection. When the connection is lost,
the request parameters are lost with it. This feature exists to allow clients to configure the sync
stream in a bandwidth-efficient way. For example, if all keys were sticky:
```
Client                         Server
  | ------{ "foo": "bar" }------> |  {"foo":"bar"}
  | <-------HTTP 200 OK---------- |
  | ------{ "baz": "quuz" }-----> | {"foo":"bar","baz":"quuz"}
  | <-------HTTP 200 OK---------- |
```
For complex nested data, APIs which include sticky parameters MUST indicate every sticky field to
avoid ambiguity. For example, an ambiguous API may state the following:
```js
{
    "foo": { // sticky
        "bar": 1,
        "baz": 2
    }
}
```
When this object is combined with an the additional object:
```js
{
    "foo": {
        "bar": 3
    }
}
```
What is the value of `baz`? Both unset and `2` are valid answers. For this reason, `baz` MUST
be marked as sticky if the desired result is `2`, else it will be unset.

Sticky request parameters SHOULD be set at the start of the connection and kept constant throughout
the lifetime of the connection. It is possible for clients and servers to disagree on the value of
a sticky request parameter in the event of packet loss:
```
             Client                         Server
               | ------{ "foo": "bar" }------> |  {"foo":"bar"}
{"foo":"bar"}  | <-------HTTP 200 OK---------- |
               | ------{ "baz": "quuz" }-----> | {"foo":"bar","baz":"quuz"}
               |      X--HTTP 200 OK---------- |
               | ------{ "baz": "quuz" }-----> | {"foo":"bar","baz":"quuz"}
               |      X--HTTP 200 OK---------- |
               | ------{ "baz": "quuz" }-----> | {"foo":"bar","baz":"quuz"}
               | <-------HTTP 200 OK---------- |
```
For this reason, some request parameters are not suitable to be made "sticky". These include parameters
which are extremely dynamic in nature, such as list ranges.

#### Sliding Window API
#### Room Subscription API

#### Extensions
We anticipate that as more features land in Matrix, different kinds of data will also want to be synced
to clients. Sync v2 did not have any first-class support to opt-in to new data. Sync v3 does have
support for this via "extensions". Extensions also allow this proposal to be broken up into more
manageable sections. Extensions are requested by the client in a dedicated `extensions` block:
```js
{
    "extensions": {
        "name_of_extension": { // sticky
            "enabled": true, // sticky
            "extension_arg": "value",
            "extension_arg_2": true
        }
    }
}
```
Extensions MUST have an `enabled` flag which defaults to `false`. If a client sends an unknown extension
name, the server MUST ignore it (or else backwards compatibility between servers is broken when a newer
client tries to communicate with an older server). Extension args may or may not be sticky, it
depends on the extension.

Extensions can leverage the data from the core API, notably which rooms are currently inside sliding
windows as well as which rooms are explicitly subscribed to.

### Extensions
#### To Device Messaging
 - Extension name: `to_device`
 - Args:
   * `limit` (Sticky): The max number of events to return per sync response.
   * `since`: The token returned in the `next_batch` section of this extension, or blank if this is the first time.
#### End-to-End Encryption
 - Extension name: `e2ee`

#### Receipts
TODO
#### Typing Notifications
TODO
#### Presence
TODO
#### Account Data
TODO





## Potential issues


## Alternatives


## Security considerations
- room sub auth check
- history visibility for timeline_limit

## Unstable prefix


## Dependencies

## Appendices
