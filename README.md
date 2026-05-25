# anilistgo

AniList API wrapper for Go.

## Install

```sh
go get github.com/Ithilias/anilistgo
```

## Usage

```go
package main

import (
	"fmt"
	"log"

	"github.com/Ithilias/anilistgo"
)

func main() {
	item, err := anilistgo.FindAnilistItem("One Piece", nil, 0)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(item.URL)
}
```

Authenticated progress updates require an AniList OAuth access token:

```go
api := anilistgo.NewAuthenticatedAPI("access-token")
err := api.UpdateProgress(21, 1000, "CURRENT")
```

Context-aware variants are available for callers that need cancellation or
custom deadlines, such as `FindAnilistItemContext`, `GetUpdatesContext`, and
`UpdateProgressContext`.

## Tests

Unit tests do not call the live AniList API by default:

```sh
go test ./...
```

Live integration tests can be enabled explicitly:

```sh
ANILISTGO_INTEGRATION=1 go test ./...
```
