# Simple

This is a simple example to show:

* Publishs webm to session
* Subscribe from session and save to disk

## Quick Start

### 1 build

```
go build main.go
```

### 2 use

```
# see ./main --help
./main -file /Volumes/vm/media/djrm480p.webm  -addr "localhost:8000" -session 'test room'
```

### 3 tips

* people in the same room will see your movie
* you will find ogg and ivf on your disk if others publish streams in the same room
