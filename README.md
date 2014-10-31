# Crispy CI

Currently just the basic workings of the start of a stripped-back CI server. A sort of sketch. But it does work! It works from `/var/lib/crispyci`, and the idea is that it'll give each job a working directory in there. Currently the run script for each job type is in there too, but should probably be in oh I don't know, `/usr/libexec/crispyci` or something. [Looks like it](http://www.linuxbase.org/betaspecs/fhs/fhs/ch04s07.html).

There's a few TODOs in there for stuff I know isn't working or don't like yet. Most significantly, there's no data store yet, and I haven't tackled multiple nodes. The most interesting problem there is streaming command output from whichever node it's running on, to wherever it needs to be served back to the user. Particularly if a user comes in half way through, and the existing and to-come data needs to be synchronised.

Recommended to develop:

```
mkdir -p .dev/libexec .dev/var
go build && ./crispyci -workingDir=.dev/var -scriptDir=.dev/libexec
```

Then you can open http://localhost:3000 in a web browser.
