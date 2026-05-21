package collector

import "errors"

var (
	errEmptyServerURL      = errors.New("collector server URL is required")
	errInvalidServerScheme = errors.New("server URL must be http(s) or ws(s)")
	errHelloRejected       = errors.New("collector hello rejected by host")
)
