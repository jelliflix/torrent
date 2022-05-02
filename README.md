Torrent
=======

Torrent finder for movies and TV series by IMDB on YTS, RARBG and The Pirate Bay.

## Usage

### Installation

#### `go get`

```shell
$ go get -u -v github.com/jelliflix/torrent
```

#### `go mod` (Recommended)

```go
import "github.com/jelliflix/torrent"
```

```shell
$ go mod tidy
```

### API

#### Magnet finder

```go
FindMovie(ctx context.Context, imdbID string) ([]Result, error)
FindEpisode(ctx context.Context, imdbID string, title string) ([]Result, error)
```

GetX returns magnet links for movie or tv episodes.

##### Examples

```go
import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/jelliflix/meta"
    "github.com/jelliflix/torrent"
)

cache := torrent.NewInMemCache()
cinemeta := meta.NewCinemeta(meta.DefaultOptions)
yts := torrent.NewYTS(torrent.DefaultYTSOpts, cache)
tpb := torrent.NewTPB(torrent.DefaultTPBOpts, cache, cinemeta)
rarbg := torrent.NewRARBG(torrent.DefaultRARBOpts, cache)
client := torrent.NewTorrent([]torrent.Client{yts, tpb, rarbg}, time.Second*10)
torrents, err := client.FindEpisode(context.Background(), "tt11650328", "Severance: Half loop")
if err != nil {
    log.Fatal(err)
}

for _, t := range torrents {
    log.Println(fmt.Sprintf("%s (%s - %d Bytes)", t.Name, t.Quality, t.Size))
}
// Output:
// Severance S01E02 Half Loop... (720p - 1449551462 Bytes)
// Severance S01E02 Half Loop... (1080p - 4378162293 Bytes)
```
