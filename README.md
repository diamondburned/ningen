# [ningen][doc]

ningen provides abstractions that override [Arikawa][arikawa]'s existing state
to provide behaviors more similar to the official client with more aggressive
caching.

## Usage

Using ningen is similar to using `*state.State`, but you'd be handling around
`*ningen.State` instead. Typically, it might look like this:

```go
s, err := state.New(os.Getenv("TOKEN"))
if err != nil {
	return errors.Wrap(err, "failed to create state")
}

n, err := ningen.FromState(s)
if err != nil {
	return errors.Wrap(err, "failed to wrap state")
}

if err := n.Open(); err != nil {
	return errors.Wrap(err, "failed to open connection to Discord")
}

return startApp(n)
```

Afterwards, `*ningen.State` can be used as if it is `*state.State`. The new
state will transparently behave more similarly to the official client.

### Extras

In addition to wrapping, `*ningen.State` also adds a few more stores that the
client can use:

- `n.NoteState` keeps track of known user notes, which can be seen on the client
  by clicking the profile picture of a user.
- `n.ReadState` allows seeing which channels are not read as well as allowing
  the client to asynchronously mark a channel as read.
- `n.MutedState` keeps track of which channels, categories and guilds are muted.
- `n.EmojiState` keeps track of the user's emojis; it returns the appropriate
  guild emojis depending on whether or not the user has Nitro.
- `n.MemberState` provides a way to lazily fetch the right-hand side member list
  seen in the official client. It also provides an asynchronous guild
  subscription API for listening to typing events.
  	- Sometimes, in large guilds, messages may not be received from the gateway.
	  This might mean that a guild subscription is required.
- `n.RelationshipState` keeps track of which users are blocked or are friends.

For detailed documentation of each state, see the [reference
documentation][doc].

[doc]: https://pkg.go.dev/github.com/diamondburned/ningen

## Markdown

ningen also provides a built-in Discord Markdown parser using
[goldmark][goldmark]. For an example on how to implement a custom Markdown
renderer that is compatible with ningen, see [md/renderer.go](https://github.com/diamondburned/ningen/blob/v1.0.0/md/renderer.go#L36).

[goldmark]: https://github.com/yuin/goldmark
