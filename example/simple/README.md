# Simple

This is a simple example to show:

* Publishs webm to session
* Subscribe from session and save to disk

## Quick Start

### 1 run

```
# save all tracks to disk
go run main.go -addr "localhost:50051" -session "test session"

# add -file to also play a file into the room (while saving tracks)
go run main.go -addr "localhost:50051" -session "test session" -file playback.webm

```

### 2 build (optional)

```
go build main.go
./main -addr "localhost:50051" -session "test session"
```


### 3 tips

* people in the same room will see your movie
* you will find ogg and ivf on your disk if others publish streams in the same room
