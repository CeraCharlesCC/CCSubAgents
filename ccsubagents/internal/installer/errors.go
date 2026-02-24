package installer

import "errors"

var ErrPinnedRequiresVersion = errors.New("--pinned requires --version")
