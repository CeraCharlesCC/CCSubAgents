package daemonapi

import (
	"io"
	"net/http"
	"os"
)

func closeIgnore(closer io.Closer) {
	if closer == nil {
		return
	}
	_ = closer.Close()
}

func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	closeIgnore(resp.Body)
}

func removeIfExists(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}
