package bootstrap

import "errors"

var ErrPinnedRequiresVersion = errors.New("--pinned requires --version")
